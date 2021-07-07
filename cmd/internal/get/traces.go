package get

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/tags"
)

var GetTracesCmd = &cobra.Command{
	Use:          "traces [AKITAURI|SERVICE]",
	Aliases:      []string{"trace"},
	Short:        "List traces for the given service.",
	Long:         "List traces in the Akita cloud, filtered by service and by tag.",
	SilenceUsage: false,
	RunE:         getTraces,
}

func init() {
	Cmd.AddCommand(GetTracesCmd)

	GetTracesCmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita service.")

	GetTracesCmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		[]string{},
		"Tag set to filter on, specified as key=value matches. Uses OR of tags.")

	GetTracesCmd.Flags().IntVar(
		&limitFlag,
		"limit",
		10,
		"Show latest N traces.")
}

func getTraces(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("Only one service permitted.")
	}

	var serviceArg string
	if len(args) == 1 {
		srcURI, err := akiuri.Parse(args[0])
		if err == nil {
			if srcURI.ObjectType != nil && !srcURI.ObjectType.IsTrace() {
				return fmt.Errorf("%q is not a trace URI.", args[0])
			}
			if srcURI.ObjectName != "" {
				return fmt.Errorf("Cannot download a trace.")
			}
			serviceArg = srcURI.ServiceName
		} else {
			// Try to use the argument as a service name
			// FIXME: maybe remove this, it is not consistent with "get specs" and is harder to
			// do there.
			serviceArg = args[0]
			printer.Infof("Attempting to use akita://%v:trace\n", serviceArg)
		}
	}

	switch {
	case serviceFlag == "" && serviceArg == "":
		return fmt.Errorf("Must specify a service name using argument or --service flag.")
	case serviceFlag == "":
		serviceFlag = serviceArg
	case serviceArg == "":
		// servceFlag is nonempty
		break
	case serviceFlag != serviceArg:
		return fmt.Errorf("Difference services specified in flag and URI.")
	}

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)

	serviceID, err := util.GetServiceIDByName(frontClient, serviceFlag)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, serviceID)
	tags, err := tags.FromPairs(tagsFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sessions, err := learnClient.ListLearnSessions(ctx, serviceID, tags)
	if err != nil {
		return err
	}

	// TODO: proper filtering of responses
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreationTime.Before(sessions[j].CreationTime)
	})

	if limitFlag > 0 {
		firstIndex := len(sessions) - limitFlag
		if firstIndex > 0 {
			printer.Infof("Showing %d most recent traces\n", limitFlag)
			sessions = sessions[firstIndex:]
		}
	}

	for _, session := range sessions {
		fmt.Printf("%-30v %-20v\n",
			session.Name,
			session.CreationTime.Format(time.RFC3339))
		for _, t := range session.Tags {
			fmt.Printf("%30v %v=%v\n", "", t.Key, t.Value)
		}
		if len(session.Tags) != 0 {
			fmt.Printf("\n")
		}
	}
	return nil

}
