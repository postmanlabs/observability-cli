package upload

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	randomdata "github.com/Pallinder/go-randomdata"
	"github.com/google/uuid"
	"github.com/logrusorgru/aurora"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/api_schema"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
)

func Run(args Args) error {
	// Resolve ServiceID
	frontClient := rest.NewFrontClient(args.Domain, args.ClientID)
	svc, err := util.GetServiceIDByName(frontClient, args.Service)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve service name %q", args.Service)
	}

	// Read spec content.
	content, err := ioutil.ReadFile(args.SpecPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read spec from %q", args.SpecPath)
	}

	// Upload
	printer.Stderr.Infof("Uploading...\n")
	specName := args.SpecName
	if specName == "" {
		specName = strings.Join([]string{
			randomdata.Adjective(),
			randomdata.Noun(),
			uuid.New().String()[0:8],
		}, "-")
	}
	req := api_schema.UploadSpecRequest{
		Name:    specName,
		Content: string(content),
	}
	ctx, cancel := context.WithTimeout(context.Background(), args.UploadTimeout)
	defer cancel()
	learnClient := rest.NewLearnClient(args.Domain, args.ClientID, svc)
	if _, err := learnClient.UploadSpec(ctx, req); err != nil {
		return errors.Wrap(err, "upload failed")
	}

	uri := akiuri.URI{
		ServiceName: args.Service,
		ObjectType:  akiuri.SPEC,
		ObjectName:  specName,
	}
	printer.Stderr.Infof("%s 🎉\n", aurora.Green("Success!"))
	printer.Stderr.Infof("Your API spec is available as: ")
	fmt.Println(uri.String()) // print URI to stdout for easy scripting

	return nil
}
