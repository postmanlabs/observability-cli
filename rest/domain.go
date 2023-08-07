package rest

import (
	"strings"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/printer"
)

// This global setting identifies which backend to use, and defaults to akita.software.
//
// If Postman credentials are used, then the domain is chosen based on the
// selected Postman environment (which may be the default or set in an
// environment variable.)
//
// If the --domain flag is used, it unconditionally overrides this choice.
//
// The special values "akitasoftware.com" and "staging.akitasoftware.com"
// need to be prefixed with an "api" to turn them into a host name.  We'll
// assume everything else is supposed to be used as-is.
//
// (The initial goal of the --domain flag was to allow per-customer instances
// of the Akita backend, i.e., myuser.akitasoftware.com and
// api.myuser.akitasoftware.com, but this usage is not supported.)

var Domain string

// Return the default domain, given the settings in use
func DefaultDomain() string {
	// Check if Postman API key in use
	key, env := cfg.GetPostmanAPIKeyAndEnvironment()
	if key == "" {
		printer.Debugf("No Postman API key, using Akita backend.\n")
		return "akita.software"
	}

	// Dispatch based on Postman environment.
	// TODO: fill in with real domains once available.
	switch strings.ToUpper(env) {
	case "BETA", "PREVIEW":
		printer.Debugf("Selecting Akita staging backend for Postman pre-production testing.\n")
		return "api.staging.akita.software"
	case "PRODUCTION", "STAGE":
		printer.Warningf("Using Akita staging backend, production not supported yet!\n")
		return "api.staging.akita.software"
	case "DEV":
		printer.Debugf("Selecting localhost backend for DEV environment.\n")
		return "localhost:50443"
	default:
		printer.Warningf("Unknown Postman environment %q, using production.\n")
		return "api.akita.software"
	}
}

// Convert domain to the specific host to contact.
func DomainToHost(domain string) string {
	switch domain {
	case "akitasoftware.com":
		return "api.akitasoftware.com"
	case "staging.akitasoftware.com":
		return "api.staging.akitasoftware.com"
	default:
		return domain
	}
}
