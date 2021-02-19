package cfg

// The single init function for cfg package to ensure initialization ordering.
func init() {
	initCfgDir()
	initCreds()
}
