package login

var (
	// Optional flags
	checkCreds bool
	showCreds bool
)

func init() {
	registerRequiredFlags()
	registerOptionalFlags()
}

func registerRequiredFlags() {
}

func registerOptionalFlags() {
	Cmd.Flags().BoolVar(
		&checkCreds,
		"check",
		false,
		"If true, checks authentication with existing credentials rather than asking for new credentials.",
	)

	Cmd.Flags().BoolVar(
		&showCreds,
		"show-creds",
		false,
		"If true, prints credentials to stdout.  Useful for debugging.",
	)
}
