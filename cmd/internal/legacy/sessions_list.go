package legacy

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/tags"

	"github.com/akitasoftware/akita-cli/cmd/internal/ci_guard"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/rest"
)

var listSessionsCmd = &cobra.Command{
	Use:   "list",
	Short: "List learn sessions",
	Long: `List learn sessions.

You may specify additional filters based on tags using --tags flag.
`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := runListSessions(); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}

var (
	listSessionsTagsFlag []string
)

func init() {
	SessionsCmd.AddCommand(ci_guard.GuardCommand(listSessionsCmd))

	listSessionsCmd.Flags().StringSliceVar(
		&listSessionsTagsFlag,
		"tags",
		nil,
		`Only return learn sessions tagged with all specified tags. Specified as a comma separated list of "key=value" pairs.`,
	)
}

func runListSessions() error {
	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(rest.Domain, clientID)

	serviceID, err := getServiceIDByName(frontClient, sessionsServiceFlag)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(rest.Domain, clientID, serviceID)
	tags, err := tags.FromPairs(listSessionsTagsFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions, err := learnClient.ListLearnSessions(ctx, serviceID, tags, 250, 0)
	if err != nil {
		return err
	}

	for _, session := range sessions {
		fmt.Println(akid.String(session.ID))
	}
	return nil
}
