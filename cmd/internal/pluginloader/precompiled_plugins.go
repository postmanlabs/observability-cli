package pluginloader

import (
	"github.com/akitasoftware/akita-cli/plugin"
)

// To include a plugin in the Akita CLI build, import the Go package
// above, and add a reference to its AkitaPluginLoader function here.
var PrecompiledPlugins map[string]plugin.AkitaPluginLoader = map[string]plugin.AkitaPluginLoader{
	// Example: "my_plugin" : myplugin.LoadAkitaPlugin,
}
