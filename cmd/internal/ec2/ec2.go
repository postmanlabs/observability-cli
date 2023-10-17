package ec2

import (
	"fmt"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	// Mandatory flag: Postman collection id
	collectionId string
)

var Cmd = &cobra.Command{
	Use:          "setup",
	Short:        "Add the Postman Live Collections Agent to the current server.",
	Long:         "The CLI will add the Postman Live Collections Agent as a systemd service to your current server.",
	SilenceUsage: true,
	RunE:         addAgentToEC2,
}

var RemoveFromEC2Cmd = &cobra.Command{
	Use:          "remove",
	Short:        "Remove the Postman Live Collections Agent from EC2.",
	Long:         "Remove a previously installed Postman agent from an EC2 server.",
	SilenceUsage: true,
	RunE:         removeAgentFromEC2,

	// Temporarily hide from users until complete
	Hidden: true,
}

func init() {
	Cmd.PersistentFlags().StringVar(&collectionId, "collection", "", "Your Postman collection ID")
	Cmd.MarkPersistentFlagRequired("collection")

	Cmd.AddCommand(RemoveFromEC2Cmd)
}

func addAgentToEC2(cmd *cobra.Command, args []string) error {
	// Check for API key
	_, err := cmderr.RequirePostmanAPICredentials("The Postman Live Collections Agent must have an API key in order to capture traces.")
	if err != nil {
		return err
	}

	// Check collecton Id's existence
	if collectionId == "" {
		return errors.New("Must specify the ID of your collection with the --collection flag.")
	}
	frontClient := rest.NewFrontClient(rest.Domain, telemetry.GetClientID())
	_, err = util.GetOrCreateServiceIDByPostmanCollectionID(frontClient, collectionId)
	if err != nil {
		return err
	}

	return setupAgentForServer(collectionId)
}

func removeAgentFromEC2(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("this command is not yet implemented")
}
