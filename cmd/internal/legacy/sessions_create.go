package legacy

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"

	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/tags"

	"github.com/akitasoftware/akita-cli/ci"
	"github.com/akitasoftware/akita-cli/cmd/internal/ci_guard"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
)

var createSessionCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a new learn session.",
	Long:         `The new learn session can be used with the learn command.`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := runCreateSession(); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

// GetSpec flags
var (
	createSessionTagsFlag   []string
	createSessionExtendFlag string
)

func init() {
	SessionsCmd.AddCommand(ci_guard.GuardCommand(createSessionCmd))

	createSessionCmd.Flags().StringSliceVar(
		&createSessionTagsFlag,
		"tags",
		nil,
		`Adds tags to the new learn session. Specified as a comma separated list of "key=value" pairs.`,
	)

	createSessionCmd.Flags().StringVar(
		&createSessionExtendFlag,
		"extend",
		"",
		`An API spec ID or version name for the API spec to expand on.

If specified, Akita will add learnings about your API from this run into the API
spec specified, allowing you to improve your API spec incrementally.

Use "latest" to specify the most recently created API spec.
`,
	)
}

func runCreateSession() error {
	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(rest.Domain, clientID)

	serviceID, err := getServiceIDByName(frontClient, sessionsServiceFlag)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(rest.Domain, clientID, serviceID)
	tags, err := tags.FromPairs(createSessionTagsFlag)
	if err != nil {
		return err
	}

	ciEnv, _, ciTags := ci.GetCIInfo()
	if ciEnv != ci.Unknown {
		printer.Stderr.Infof("Detected CI environment: %s\n", ciEnv)
		for k, v := range ciTags {
			tags[k] = v
		}
	}

	var baseSpecRef *kgxapi.APISpecReference
	if createSessionExtendFlag != "" {
		var id akid.APISpecID
		if err := akid.ParseIDAs(createSessionExtendFlag, &id); err == nil {
			baseSpecRef = &kgxapi.APISpecReference{ID: &id}
		} else {
			baseSpecRef = &kgxapi.APISpecReference{Version: &createSessionExtendFlag}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Custom session name not supported in old CLI.
	sessionID, err := learnClient.CreateLearnSession(ctx, baseSpecRef, "", tags)
	if err != nil {
		return err
	}

	printer.Stderr.Infof("New session created with ID: ")
	fmt.Println(akid.String(sessionID))
	return nil
}
