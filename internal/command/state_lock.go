// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package command

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform/internal/command/arguments"
	"github.com/hashicorp/terraform/internal/command/clistate"
	"github.com/hashicorp/terraform/internal/command/views"
	"github.com/hashicorp/terraform/internal/states/statemgr"

	"github.com/hashicorp/cli"
	"github.com/hashicorp/terraform/internal/tfdiags"
)

// StateLockCommand is a cli.Command implementation that manually locks
// the state.
type StateLockCommand struct {
	Meta
	StateMeta
}

func (c *StateLockCommand) Run(args []string) int {
	args = c.Meta.process(args)
	var statePath string
	cmdFlags := c.Meta.defaultFlagSet("state lock")
	cmdFlags.DurationVar(&c.Meta.stateLockTimeout, "lock-timeout", 0, "lock timeout")
	cmdFlags.StringVar(&statePath, "state", "", "path")
	if err := cmdFlags.Parse(args); err != nil {
		c.Ui.Error(fmt.Sprintf("Error parsing command-line flags: %s\n", err.Error()))
		return cli.RunResultHelp
	}
	args = cmdFlags.Args()

	if statePath != "" {
		c.Meta.statePath = statePath
	}

	// assume everything is initialized. The user can manually init if this is
	// required.
	configPath, err := ModulePath(args)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	var diags tfdiags.Diagnostics

	backendConfig, backendDiags := c.loadBackendConfig(configPath)
	diags = diags.Append(backendDiags)
	if diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	// Load the backend
	b, backendDiags := c.Backend(&BackendOpts{
		Config: backendConfig,
	})
	diags = diags.Append(backendDiags)
	if backendDiags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	env, err := c.Workspace()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error selecting workspace: %s", err))
		return 1
	}
	stateMgr, err := b.StateMgr(env)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Failed to load state: %s", err))
		return 1
	}

	_, isLocal := stateMgr.(*statemgr.Filesystem)
	if isLocal {
		c.Ui.Error("Local state locking is redundant.")
		return 0
	}

	stateLocker := clistate.NewLocker(c.stateLockTimeout, views.NewStateLocker(arguments.ViewHuman, c.View))
	if diags := stateLocker.Lock(stateMgr, "state-lock"); diags.HasErrors() {
		c.showDiagnostics(diags)
		return 1
	}

	c.Ui.Output(c.Colorize().Color(strings.TrimSpace(fmt.Sprintf(outputStateLockSuccess, stateLocker.LockID()))))
	return 0
}

func (c *StateLockCommand) Help() string {
	helpText := `
Usage: terraform [global options] state lock

  Manually lock the state for the defined configuration.

  This will not modify your infrastructure. This command adds a lock on the
  state for the current workspace. The behavior of this lock is dependent
  on the backend being used. Local state files cannot be locked.
`
	return strings.TrimSpace(helpText)
}

func (c *StateLockCommand) Synopsis() string {
	return "Acquire a lock on the current workspace"
}

const outputStateLockSuccess = `
LOCKID: %s

[reset][bold][green]Terraform state has been successfully been locked![reset][green]

The state has been locked, and Terraform commands should now be blocked from
obtaining a new lock on the remote state.
`
