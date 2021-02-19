package pluginloader

import (
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/plugin/akita"
)

func Load(paths []string) ([]plugin.AkitaPlugin, error) {
	// TODO(kku): actually load plugins from so files.
	var loaded []plugin.AkitaPlugin
	if akita.OfficialPlugin != nil {
		loaded = append(loaded, akita.OfficialPlugin)
	}
	return loaded, nil
}
