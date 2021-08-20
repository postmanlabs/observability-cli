package pluginloader

import (
	go_plugin "plugin"

	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/plugin/akita"
)

func Load(paths []string) ([]plugin.AkitaPlugin, error) {
	var loaded []plugin.AkitaPlugin

	for _, path := range paths {
		if loader, found := PrecompiledPlugins[path]; found {
			plug, err := loader()
			if err != nil {
				return nil, errors.Wrapf(err, "failed to load %q", path)
			}
			loaded = append(loaded, plug)
			continue
		}

		// Preserve this path, but note that it really doesn't work for a plugin
		// that was not compiled to refer to the same source paths that akita-cli was
		// compiled from.
		p, err := go_plugin.Open(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to open plugin %q", path)
		}

		sym, err := p.Lookup(plugin.AkitaPluginLoaderName)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to find %s in %q", plugin.AkitaPluginLoaderName, path)
		}

		if loader, ok := sym.(plugin.AkitaPluginLoader); ok {
			plug, err := loader()
			if err != nil {
				return nil, errors.Wrapf(err, "failed to load %q", path)
			}
			loaded = append(loaded, plug)
		} else {
			return nil, errors.Wrapf(err, "%s does not implement AkitaPluginLoader in %q", plugin.AkitaPluginLoaderName, path)
		}
	}

	if akita.OfficialPlugin != nil {
		loaded = append(loaded, akita.OfficialPlugin)
	}
	return loaded, nil
}
