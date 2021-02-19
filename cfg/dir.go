package cfg

import (
	"os"
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

	if stat, err := os.Stat(cfgDir); os.IsNotExist(err) {
		// Create the directory if it doesn't exist.
		if err := os.Mkdir(cfgDir, 0700); err != nil {
			printer.Stderr.Warningf("Failed to create config directory %s, persistent config will not work, error: %v\n", cfgDir, err)
		}
	} else if err != nil {
		printer.Stderr.Errorf("Failed to stat %s: %v\n", cfgDir, err)
		os.Exit(1)
	} else if !stat.IsDir() {
		// For legacy users of superfuzz-cli, `$HOME/.akita` was a file rather than
		// a directory. The set of potential users with this problem should be
		// exactly the set of employees at akita.
		printer.Stderr.Errorf("%s is not a directory, please remove.\n", cfgDir)
		os.Exit(1)
	}
}
