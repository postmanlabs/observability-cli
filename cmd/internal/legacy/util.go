package legacy

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"

	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/tags"
)

func printViewSpecMessage(svc akid.ServiceID, svcName string, spec akid.APISpecID, specName string) {
	editorURL := url.URL{
		Scheme: "https",
		Host:   getAppHost(),
		Path:   path.Join("/service", akid.String(svc), "/spec", akid.String(spec)),
	}
	if viper.GetBool("test_only_disable_https") {
		editorURL.Scheme = "http"
	}

	// Print spec ID to stdout to make it easy for scripting.
	// We precede it with a message on stderr so when the user is using the CLI
	// interactively, it doesn't look like there's a random spec ID floating
	// around.
	outURI := akiuri.URI{
		ServiceName: svcName,
		ObjectName: specName,
		ObjectType: akiuri.SPEC.Ptr(),
	}

	// Print spec URI to stdout to make it easy for scripting.
	// We precede it with a message on stderr so when the user is using the CLI
	// interactively, it doesn't look like there's a random spec ID floating
	// around.
	printer.Stderr.Infof("Your API spec URL is: ")
	fmt.Println(outURI.String())

	successMsg := printer.Color.Green(fmt.Sprintf("🔎 View your spec at: %s", editorURL.String()))
	printer.Stderr.Infof("%s 🎉\n\n%s\n\n", printer.Color.Green("Success!"), successMsg)
}

func getAppHost() string {
	// Special case editor URL setting for Akita internal staging environment. The app is hosted at a
	// domain which does not follow normal conventions.
	if akiflag.Domain == "staging.akita.software" {
		return "app.staging.akita.software"
	} else {
		return "app." + akiflag.Domain
	}
}

func getServiceIDByName(c rest.FrontClient, name string) (akid.ServiceID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	services, err := c.GetServices(ctx)
	if err != nil {
		return akid.ServiceID{}, errors.Wrap(err, "failed to get list of services associated with the account")
	}

	var serviceID akid.ServiceID
	for _, svc := range services {
		if svc.Name == name {
			serviceID = svc.ID
			break
		}
	}
	if serviceID == (akid.ServiceID{}) {
		return akid.ServiceID{}, errors.Errorf("cannot determine service ID for %s", name)
	}
	return serviceID, nil
}

func startLearnSession(c rest.LearnClient, baseSpecRef *kgxapi.APISpecReference, tags map[tags.Key]string) (akid.LearnSessionID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Session name not supported in old CLI.
	lrn, err := c.CreateLearnSession(ctx, baseSpecRef, "", tags)
	if err != nil {
		return akid.LearnSessionID{}, errors.Wrap(err, "failed to start learn session")
	}
	return lrn, nil
}

func checkpointLearnSession(c rest.LearnClient, lrn akid.LearnSessionID, timeout time.Duration) (akid.APISpecID, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	name := util.RandomAPIModelName()
	specID, err := c.CreateSpec(ctx, name, []akid.LearnSessionID{lrn}, rest.CreateSpecOptions{})
	if err != nil {
		return akid.APISpecID{}, "", errors.Wrapf(err, "failed to checkpoint learn session %s", akid.String(lrn))
	}
	return specID, name, nil
}
