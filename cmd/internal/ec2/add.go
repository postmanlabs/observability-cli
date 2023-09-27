package ec2

import (
	"context"
	"os/exec"

	// "github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	// "github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/pkg/errors"
)

// Helper function for reporting telemetry
func reportStep(stepName string) {
	telemetry.WorkflowStep("Add to EC2", stepName)
}

// A function which executes the next part of the workflow,
// and picks a next state (Some) or exits (None), or signals an eror.
type AddWorkflowState func(*AddWorkflow) (next optionals.Optional[AddWorkflowState], err error)

// Helper functions for choosing the next state.
func awf_done() (optionals.Optional[AddWorkflowState], error) {
	// I do not understand why Go cannot infer this type.
	return optionals.None[AddWorkflowState](), nil
}

func awf_error(err error) (optionals.Optional[AddWorkflowState], error) {
	return optionals.None[AddWorkflowState](), err
}

func awf_next(f AddWorkflowState) (optionals.Optional[AddWorkflowState], error) {
	return optionals.Some[AddWorkflowState](f), nil
}

type AddWorkflow struct {
	currentState AddWorkflowState
	ctx          context.Context
}

// Run the "add to EC2" workflow until we complete or get an error.
// Errors that are UsageErrors should be returned as-is; other
// errors should be wrapped to avoid showing usage.
func RunAddWorkflow() error {
	wf := &AddWorkflow{
		currentState: initState,
		ctx:          context.Background(),
	}

	nextState := optionals.Some[AddWorkflowState](initState)
	var err error = nil
	for nextState.IsSome() && err == nil {
		wf.currentState, _ = nextState.Get()
		nextState, err = wf.currentState(wf)
	}
	if err == nil {
		telemetry.Success("Add to EC2")
	} else if errors.Is(err, terminal.InterruptErr) {
		printer.Infof("Interrupted!\n")
		telemetry.WorkflowStep("Add to EC2", "User interrupted session")
		return nil
	} else if _, ok := err.(UsageError); ok {
		telemetry.Error("Add to EC2", err)
		return err
	} else {
		telemetry.Error("Add to EC2", err)
		return cmderr.AkitaErr{Err: err}
	}
	return err
}

// State machine ASCII art:
//
//         init
//           |
//           V
//       preChecks
//           |
//           V
//     configureSystemd
//           |
//           V
//     enablePostmanAgent
//           |
//           V
//       postChecks

func initState(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("EC2:Start Add to EC2")

	return awf_next(preChecks)
}

// Run pre-checks
func preChecks(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("EC2:Running pre-checks")

	// Check if systemd exists
	_, serr := exec.LookPath("systemd")
	if serr != nil {
		printer.Errorf("Could not find systemd binary in your OS\n")
		return awf_error(errors.New("We don't have support for non-systemd OS as of now; For more information please contact observability-support@postman.com for assistance."))
	}

	// TODO: Check for correct user r/w permissions too ?
	// TOOD: Check capabilities enabled for agent to work ?
	return awf_next(configureSystemd)
}

// Writes systemd files
func configureSystemd(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("EC2:Writing systemd files")
	return awf_next(enablePostmanAgent)
}

// Starts the postman LCA agent as a systemd service
func enablePostmanAgent(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("Enabling postman-lc-agent as a service")
	return awf_next(postChecks)
}

// Run post-checks
func postChecks(wf *AddWorkflow) (nextState optionals.Optional[AddWorkflowState], err error) {
	reportStep("EC2:Running post checks")

	// Verify if traffic is captured
	return awf_done()
}
