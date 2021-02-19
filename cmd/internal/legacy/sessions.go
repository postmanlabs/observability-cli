package legacy

import (
	"github.com/spf13/cobra"
)

// Exposed so cliv2 can reuse it as a hidden command for backward compatibility.
var SessionsCmd = &cobra.Command{
	Use:          "learn-sessions",
	Short:        "Manage learn sessions.",
	Long:         "Learn sessions are basic units of organization for Akita's witness collection.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	sessionsServiceFlag string
)

func init() {
	SessionsCmd.PersistentFlags().StringVar(
		&sessionsServiceFlag,
		"service",
		"",
		"Your Akita service.",
	)
	cobra.MarkFlagRequired(SessionsCmd.PersistentFlags(), "service")
}
