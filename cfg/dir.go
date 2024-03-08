package cfg

import (
	"path/filepath"

	homedir "github.com/mitchellh/go-homedir"

	"github.com/akitasoftware/akita-cli/printer"
)

var (
	cfgDir string
)

func initCfgDir() {
	home, err := homedir.Dir()
	if err != nil {
		printer.Stderr.Warningf("Failed to find $HOME, defaulting to '.', error: %v", err)
		home = "."
	}
	cfgDir = filepath.Join(filepath.Join(home, ".akita"))
}
