package upload

import (
	"time"

	"github.com/spf13/cobra"
)

var (
	// Required flags
	serviceFlag string

	// Optional flags
	specNameFlag      string
	uploadTimeoutFlag time.Duration
)

func init() {
	//
	// Required Flags
	//
	Cmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Serivce that the spec belongs to.")
	cobra.MarkFlagRequired(Cmd.Flags(), "service")

	//
	// Optional Flags
	//
	Cmd.Flags().StringVar(
		&specNameFlag,
		"name",
		"",
		"A custom name for the spec.")

	Cmd.Flags().DurationVar(
		&uploadTimeoutFlag,
		"timeout",
		3*time.Minute,
		"Timeout for spec upload.")
}
