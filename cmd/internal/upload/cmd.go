package upload

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/upload"
)

var Cmd = &cobra.Command{
	Use:          "upload [FILE...]",
	Short:        "Upload an API model or a set of traces to Akita.",
	SilenceUsage: true,
	Args:         cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// At least one of --dest or --service must be given.
		if destFlag == "" && serviceFlag == "" {
			return errors.New("required flag \"dest\" not set")
		}

		// At most one of --dest or --service can be given.
		if destFlag != "" && serviceFlag != "" {
			return errors.New("cannot set both \"dest\" and \"service\" flags")
		}

		// If --name is given, then --dest cannot be given.
		if specNameFlag != "" && destFlag != "" {
			return errors.New("\"name\" flag cannot be used with \"dest\" flag")
		}

		// Rewrite --service and --name into --dest.
		if destFlag == "" {
			destFlag = "akita://" + serviceFlag + ":spec"
			if specNameFlag != "" {
				destFlag += ":" + specNameFlag
			}
		}

		// Parse --dest.
		destURI, err := akiuri.Parse(destFlag)
		if err != nil {
			return errors.Wrapf(err, "%q is not a well-formed AkitaURI", destFlag)
		}

		// Destination must specify an object type.
		if destURI.ObjectType == nil {
			return errors.New("\"dest\" must specify an object type. For example, \"akita://serviceName:trace\"")
		}

		// If more than one file is given, then the object type must be "trace".
		if len(args) > 1 && destURI.ObjectType.IsSpec() {
			return errors.New("can only upload one API model at a time")
		}

		// If --append is given, then the object type must be "trace".
		if appendFlag && !destURI.ObjectType.IsTrace() {
			return errors.New("\"append\" can only be used with trace objects")
		}

		// If --include-trackers is given, then the object type must be "trace".
		if includeTrackersFlag && !destURI.ObjectType.IsTrace() {
			return errors.New("\"append\" can only be used with trace objects")
		}

		// If --plugins is given, then the object type must be "trace".
		if pluginsFlag != nil && !destURI.ObjectType.IsTrace() {
			return errors.New("\"plugins\" can only be used with trace objects")
		}

		uploadArgs := upload.Args{
			ClientID:      akid.GenerateClientID(),
			Domain:        akiflag.Domain,
			DestURI:       destURI,
			FilePaths:     args,
			Append:        appendFlag,
			UploadTimeout: uploadTimeoutFlag,
		}

		if err := upload.Run(uploadArgs); err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	},
}
