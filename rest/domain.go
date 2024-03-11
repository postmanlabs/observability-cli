package rest

import (
	"strings"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/printer"
)

// This global setting identifies which back end to use, and defaults to
// api.observability.postman.com.
//
// The domain is chosen based on the selected Postman environment (which may be
// the default or set in an environment variable.)
//
// If the --domain flag is used, it unconditionally overrides this choice.
var Domain string

// Return the default domain, given the settings in use
func DefaultDomain() string {
	_, env := cfg.GetPostmanAPIKeyAndEnvironment()

	// Dispatch based on Postman environment.
	switch strings.ToUpper(env) {
	case "":
		// Not specified by user, default to PRODUCTION
		return "api.observability.postman.com"
	case "DEV":
		printer.Debugf("Selecting localhost backend for DEV environment.\n")
		return "localhost:50443"
	case "BETA":
		printer.Debugf("Selecting Postman beta backend for pre-production testing.\n")
		return "api.observability.postman-beta.com"
	case "PREVIEW":
		printer.Debugf("Selecting Postman preview backend for pre-production testing.\n")
		return "api.observability.postman-preview.com"
	case "STAGE":
		printer.Debugf("Selecting Postman staging backend for pre-production testing.\n")
		return "api.observability.postman-stage.com"
	case "PRODUCTION":
		printer.Debugf("Selecting Postman production backend.\n")
		return "api.observability.postman.com"
	default:
		printer.Warningf("Unknown Postman environment %q, using production.\n")
		return "api.observability.postman.com"
	}
}
