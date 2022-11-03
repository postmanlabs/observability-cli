package akiflag

import (
	"fmt"

	"github.com/spf13/pflag"
)

// Rename a flag by creating 2 flags that share the same variable:
// - one that uses the old name but is deprecated
// - one that uses the new name

func RenameStringFlag(fs *pflag.FlagSet, flagVar *string, oldName, newName, defaultVal, usage string) {
	fs.StringVar(flagVar, oldName, defaultVal, usage)
	fs.StringVar(flagVar, newName, defaultVal, usage)
	fs.MarkDeprecated(oldName, fmt.Sprintf("use --%s instead.", newName))
}

func RenameStringSliceFlag(fs *pflag.FlagSet, flagVar *[]string, oldName, newName string, defaultVal []string, usage string) {
	fs.StringSliceVar(flagVar, oldName, defaultVal, usage)
	fs.StringSliceVar(flagVar, newName, defaultVal, usage)
	fs.MarkDeprecated(oldName, fmt.Sprintf("use --%s instead.", newName))
}

func RenameIntFlag(fs *pflag.FlagSet, flagVar *int, oldName, newName string, defaultVal int, usage string) {
	fs.IntVar(flagVar, oldName, defaultVal, usage)
	fs.IntVar(flagVar, newName, defaultVal, usage)
	fs.MarkDeprecated(oldName, fmt.Sprintf("use --%s instead.", newName))
}
