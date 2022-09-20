package legacy

import (
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/ci_guard"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
)

var checkpointSessionCmd = &cobra.Command{
	Use:          "checkpoint [Session ID]",
	Short:        "Converts learn session into an API spec.",
	Long:         `Converts all witnesses collected so far for a learn session into an API spec`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// args[0] is guaranteed to work due to ExactArgs(1)
		if err := runCheckpointSession(args[0]); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

// Checkpoint flags
var (
	checkpointSessionTimeoutFlag time.Duration
)

func init() {
	SessionsCmd.AddCommand(ci_guard.GuardCommand(checkpointSessionCmd))

	checkpointSessionCmd.Flags().DurationVar(
		&checkpointSessionTimeoutFlag,
		"timeout",
		60*time.Second, // matches ALB gateway timeout
		"Timeout for the checkpoint",
	)
}

func runCheckpointSession(rawSessionID string) error {
	var sessionID akid.LearnSessionID
	if err := akid.ParseIDAs(rawSessionID, &sessionID); err != nil {
		return errors.Wrapf(err, "failed to parse learn session ID %s", rawSessionID)
	}

	args := apispec.Args{
		ClientID: telemetry.GetClientID(),
		Domain:   rest.Domain,
		Service:  sessionsServiceFlag,
		Timeout:  &checkpointSessionTimeoutFlag,

		GetSpecEnableRelatedFields: getSpecEnableRelatedFieldsFlag,

		LearnSessionID: &sessionID,
	}
	if err := apispec.Run(args); err != nil {
		return cmderr.AkitaErr{Err: err}
	}
	return nil
}
