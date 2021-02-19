package plugin

import (
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
)

// Interface implemented by all plugins.
type AkitaPlugin interface {
	// Name of the plugin.
	Name() string

	// Performs in place transformation on the given IR.
	// Returns a non-nil error if the error is not recoverable and all processing
	// should halt.
	Transform(*pb.Method) error
}

// Every plugin must export a function called "LoadAkitaPlugin" of type
// AkitaPluginLoader.
const (
	AkitaPluginLoaderName = "LoadAkitaPlugin"
)

type AkitaPluginLoader = func() (AkitaPlugin, error)
