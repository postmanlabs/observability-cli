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
)

var GetTracesCmd = &cobra.Command{
	Use:          "traces [AKITAURI|SERVICE]",
	Aliases:      []string{"trace"},
	Short:        "List traces for the given project.",
	Long:         "List traces in the Akita cloud, filtered by project and by tag.",
	SilenceUsage: false,
	RunE:         getTraces,
}

func init() {
	Cmd.AddCommand(GetTracesCmd)

	GetTracesCmd.Flags().StringVar(
		&serviceFlag,
		"project",
		"",
		"Your Akita project.")

	GetTracesCmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita project.  DEPRECATED, prefer --project.")

	GetTracesCmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita cluster (alias for 'project').")

	GetTracesCmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		[]string{},
		"Tag set to filter on, specified as key=value pairs. All tags must match.")

	GetTracesCmd.Flags().IntVar(
		&limitFlag,
		"limit",
		10,
		"Show latest N traces.")
}

func getTraces(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("Only one project permitted.")
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
		return fmt.Errorf("Must specify a project name using argument or --project flag.")
	case serviceFlag == "":
		serviceFlag = serviceArg
	case serviceArg == "":
		// servceFlag is nonempty
		break
	case serviceFlag != serviceArg:
		return fmt.Errorf("Different projects specified in flag and URI.")
	}

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)

	serviceID, err := util.GetServiceIDByName(frontClient, serviceFlag)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, serviceID)
	tags, err := util.ParseTags(tagsFlag)
	if err != nil {
		return err
	}

	// TODO: ListLearnSessions has a limit, but currently the back-end applies that limit
	// before ensuring all tags match, instead of after.  Once we fix that, we can
	// push the limit and sort to the backend.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sessions, err := learnClient.ListLearnSessions(ctx, serviceID, tags)
	if err != nil {
		return err
	}

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
