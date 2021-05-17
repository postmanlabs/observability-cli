package kafka

import (
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	kafkaCmd "github.com/akitasoftware/akita-cli/kafka"
)

var Cmd = &cobra.Command{
	Use:   "kafka",
	Short: "Capture messages from Kafka traffic.",
	Long:  "Capture messages from Kafka traffic.",

	RunE: func(cmd *cobra.Command, _ []string) error {
		// TODO: arguments

		if err := kafkaCmd.Run(); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}
