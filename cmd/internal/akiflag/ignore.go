package akiflag

import (
	"time"

	"github.com/spf13/pflag"
)

// Convenience functions for creating legacy command-line flags whose values are
// ignored.

func IgnoreDurationFlags(fs *pflag.FlagSet, flagNames []string, usage string) {
	var ignored time.Duration
	for _, flagName := range flagNames {
		fs.DurationVar(
			&ignored,
			flagName,
			0,
			usage,
		)
		fs.MarkDeprecated(flagName, "and is now ignored. Please remove this flag from your command.")
	}
}

func IgnoreIntFlags(fs *pflag.FlagSet, flagNames []string, usage string) {
	var ignored int
	for _, flagName := range flagNames {
		fs.IntVar(
			&ignored,
			flagName,
			0,
			usage,
		)
		fs.MarkDeprecated(flagName, "and is now ignored. Please remove this flag from your command.")
	}
}

func IgnoreStringFlags(fs *pflag.FlagSet, flagNames []string, usage string) {
	var ignored string
	for _, flagName := range flagNames {
		fs.StringVar(
			&ignored,
			flagName,
			"",
			usage,
		)
		fs.MarkDeprecated(flagName, "and is now ignored. Please remove this flag from your command.")
	}
}
