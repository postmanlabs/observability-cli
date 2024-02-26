package get

import (
	"fmt"

	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Parent command for listing objects from Akita.
var Cmd = &cobra.Command{
	Deprecated:   "This is no longer supported and will be removed in a future release.",
	Use:          "get [AKITAURI]",
	Short:        "List or download objects in the Akita cloud.",
	Long:         "List or download objects in the Akita cloud.",
	SilenceUsage: true,
	RunE:         getByURIType,
}

func getByURIType(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	srcURI, err := akiuri.Parse(args[0])
	if err != nil {
		return errors.Wrapf(err, "%q is not an Akita URI", args[0])
	}

	switch {
	case srcURI.ObjectType == nil:
		return fmt.Errorf("Must specify a subcommand or an AkitaURI with a type.")
	case srcURI.ObjectType.IsTrace():
		return getTraces(cmd, args)
	case srcURI.ObjectType.IsSpec():
		return getSpecs(cmd, args)
	default:
		return fmt.Errorf("Unhandled URI type in %q", srcURI)
	}
}
