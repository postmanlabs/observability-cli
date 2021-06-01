package setversion

import (
	"context"
	"time"

	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
)

func Run(args Args) error {
	// Resolve service ID.
	frontClient := rest.NewFrontClient(args.Domain, args.ClientID)
	serviceName := args.ModelURI.ServiceName
	serviceID, err := util.GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		return err
	}

	// Resolve API model ID.
	learnClient := rest.NewLearnClient(args.Domain, args.ClientID, serviceID)
	modelID, err := util.ResolveSpecURI(learnClient, args.ModelURI)
	if err != nil {
		return err
	}

	// Set version name.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return learnClient.SetSpecVersion(ctx, modelID, args.VersionName)
}
