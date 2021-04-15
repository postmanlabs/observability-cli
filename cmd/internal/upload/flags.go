package upload

import (
	"time"
)

var (
	// Required flags – exactly one of these must be specified.
	destFlag    string
	serviceFlag string // deprecated

	// Optional flags
	specNameFlag string // deprecated

	appendFlag          bool
	includeTrackersFlag bool
	overwriteFlag       bool
	uploadTimeoutFlag   time.Duration

	pluginsFlag []string
)

const DEFAULT_TIMEOUT = 3 * time.Minute

func init() {
	//
	// Required Flags
	//
	Cmd.Flags().StringVar(
		&destFlag,
		"dest",
		"",
		"The Akita URI to upload to. The URI must specify at least the service name and the object type for the upload.")
	// Not marked as required because users can still fall back on the deprecated
	// --service flag.
	//
	//cobra.MarkFlagRequired(Cmd.Flags(), "dest")

	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Service that the spec belongs to.")
	Cmd.Flags().MarkDeprecated("service", "use --dest instead.")

	//
	// Optional Flags
	//
	Cmd.Flags().StringVar(
		&specNameFlag,
		"name",
		"",
		"A custom name for the spec.")
	Cmd.Flags().MarkDeprecated("name", "use --dest instead.")

	Cmd.Flags().BoolVar(
		&appendFlag,
		"append",
		false,
		"Add the upload to an existing Akita trace.")

	Cmd.Flags().BoolVar(
		&includeTrackersFlag,
		"include-trackers",
		false,
		"If set to true, disables automatic filtering of requests to third-party trackers that are recorded in traces.",
	)

	Cmd.Flags().DurationVar(
		&uploadTimeoutFlag,
		"timeout",
		DEFAULT_TIMEOUT,
		"Timeout for spec upload.")

	Cmd.Flags().StringSliceVar(
		&pluginsFlag,
		"plugins",
		nil,
		"Paths of third-party Akita plugins. These are executed in the order given.",
	)
	Cmd.Flags().MarkHidden("plugins")
}
