package pluginloader

import (
	"github.com/akitasoftware/akita-cli/plugin"

	"github.com/akitasoftware/plugin-flickr"
)

// To include a plugin in the Akita CLI build, import the Go package
// above, and add a reference to its AkitaPluginLoader function here.
var PrecompiledPlugins map[string]plugin.AkitaPluginLoader = map[string]plugin.AkitaPluginLoader{
	// Example: "my_plugin" : myplugin.LoadAkitaPlugin,

	// Plugin for transforming Flickr's API into a more REST-like format.
	"flickr": plugin_flickr.LoadAkitaPlugin,
}
