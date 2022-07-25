package get

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/akitasoftware/akita-cli/apispec"
	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/tags"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var GetSpecsCmd = &cobra.Command{
	Use:          "spec [AKITAURI [FILE]]",
	Aliases:      []string{"specs", "model", "models"},
	Short:        "List or download specifications for a project.",
	Long:         "List specifications in the Akita cloud, filtered by project and by tag. Or, specify a particular spec to download it.",
	SilenceUsage: false,
	RunE:         getSpecs,
}

func init() {
	Cmd.AddCommand(GetSpecsCmd)

	GetSpecsCmd.Flags().StringVar(
		&serviceFlag,
		"project",
		"",
		"Your Akita project.")

	GetSpecsCmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita project.  DEPRECATED, prefer --project.")

	GetSpecsCmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita project. DEPRECATED, prefer --project.")

	GetSpecsCmd.Flags().StringSliceVar(
		&tagsFlag,
		"tags",
		[]string{},
		"Tag set to filter on, specified as key=value pairs. All tags must match.")

	GetSpecsCmd.Flags().IntVar(
		&limitFlag,
		"limit",
		10,
		"Show latest N specs.")
}

// All expected tags and values are present in spec.  Other tags may be
// present, and other values than the one provided may be present for
// the given tags.  Uses the multi-valued tag set when present, falling
// back to the deprecated single-valued tag set otherwise.
func allTagsMatch(spec *kgxapi.SpecInfo, expected map[tags.Key]string) bool {
	// Use the multi-valued tags map if present.  Fall back to the single
	// valued map otherwise.
	tagMap := spec.TagsSet
	if len(tagMap) == 0 {
		tagMap = spec.Tags.AsTags()
	}
	// handle nil tags from REST call
	if len(spec.Tags) == 0 {
		return len(expected) == 0
	}

	for k, v := range expected {
		// Continue if k is present and v is in Tags[k]; return false
		// otherwise.
		if specValues, ok := tagMap[k]; ok {
			if _, exists := specValues[v]; exists {
				continue
			}
		}
		return false
	}
	return true
}

func listSpecs(src akiuri.URI, tags map[tags.Key]string, limit int) error {
	printer.Debugf("Listing specs for %q with tags %v and limit %v\n", src, tags, limit)

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)

	serviceID, err := util.GetServiceIDByName(frontClient, src.ServiceName)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, serviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	specs, err := learnClient.ListSpecs(ctx)
	if err != nil {
		return err
	}

	if len(specs) == 0 {
		printer.Warningf("No specs found for project %q.\n", src.ServiceName)
		return nil
	}

	// TODO: the ListSpecs API does not do any filtering, but we do have a
	// database function (FindAPISpecsForService) that knows how to filter by tag.
	// Switch the API to start using that functions and add tag and limit arguments.
	if len(tags) > 0 {
		filteredSpecs := make([]kgxapi.SpecInfo, 0)
		for _, s := range specs {
			if allTagsMatch(&s, tags) {
				filteredSpecs = append(filteredSpecs, s)
			}
		}
		specs = filteredSpecs
	}

	if len(specs) == 0 {
		printer.Warningf("No specs found with matching tag.\n", src.ServiceName)
		return nil
	}

	sort.Slice(specs, func(i, j int) bool {
		return specs[i].EditTime.Before(specs[j].EditTime)
	})

	if limit > 0 {
		firstIndex := len(specs) - limit
		if firstIndex > 0 {
			printer.Infof("Showing %d of %d matching specs.\n", limit, len(specs))
			specs = specs[firstIndex:]
		}
	}

	for _, spec := range specs {
		fmt.Printf("%-30s %-20v %-10s %s\n",
			spec.Name,
			spec.EditTime.Format(time.RFC3339),
			spec.State,
			strings.Join(spec.VersionTags, ","))
		for k, v := range spec.Tags {
			fmt.Printf("%30v %v=%v\n", "", k, v)
		}
		if len(spec.Tags) != 0 {
			fmt.Printf("\n")
		}
	}
	return nil
}

func downloadSpec(srcURI akiuri.URI, outputFile string) error {
	printer.Debugf("Downloading specs %q to file %q\n", srcURI, outputFile)

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)

	serviceID, err := util.GetServiceIDByName(frontClient, srcURI.ServiceName)
	if err != nil {
		return err
	}

	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, serviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	id, err := learnClient.GetAPISpecIDByName(ctx, srcURI.ObjectName)
	if err != nil {
		return errors.Wrapf(err, "Couldn't find spec %q", srcURI.ObjectName)
	}
	if (id == akid.APISpecID{}) {
		return fmt.Errorf("No such spec name %q", srcURI.ObjectName)
	}

	// TODO: make this a flag?
	resp, err := learnClient.GetSpec(ctx, id, rest.GetSpecOptions{
		EnableRelatedTypes: false,
	})
	if err != nil {
		return errors.Wrapf(err, "Error downloading spec %q", srcURI.ObjectName)
	}

	if len(resp.Content) == 0 {
		return errors.Wrapf(err, "Spec %q is empty", srcURI.ObjectName)
	}

	output := os.Stdout
	if outputFile != "" {
		var err error
		output, err = os.Create(outputFile)
		if err != nil {
			return errors.Wrapf(err, "Error creating file %q", outputFile)
		}
		defer output.Close()
	}

	return apispec.WriteSpec(output, resp.Content)
}

func getSpecs(cmd *cobra.Command, args []string) error {
	if len(args) > 2 {
		return errors.New("Only one source and one destination supported.")
	}

	tags, err := util.ParseTags(tagsFlag)
	if err != nil {
		return err
	}

	var srcURI akiuri.URI
	if len(args) > 0 {
		var err error
		srcURI, err = akiuri.Parse(args[0])
		if err != nil {
			return errors.Wrapf(err, "%q is not a well-formed AkitaURI", args[0])
		}
		if srcURI.ObjectType == nil {
			srcURI.ObjectType = akiuri.SPEC.Ptr()
		} else if !srcURI.ObjectType.IsSpec() {
			return fmt.Errorf("%q is not a spec URI", args[0])
		}
		if serviceFlag != "" && srcURI.ServiceName != serviceFlag {
			return errors.New("Project name does not match URI.")
		}
	} else {
		// Use --project flag to list instead
		if serviceFlag == "" {
			return errors.New("Must specify an akitaURI or project name.")
		}

		srcURI.ServiceName = serviceFlag
		srcURI.ObjectType = akiuri.SPEC.Ptr()
		srcURI.ObjectName = ""
	}

	// If no object name, then list
	if srcURI.ObjectName == "" {
		err = listSpecs(srcURI, tags, limitFlag)
		if err != nil {
			return cmderr.AkitaErr{Err: err}
		}
		return nil
	}

	// Download to stdout or file
	var outputFile string
	if len(args) > 1 {
		outputFile = args[1]
	}
	err = downloadSpec(srcURI, outputFile)
	if err != nil {
		return cmderr.AkitaErr{Err: err}
	}

	return nil

}
