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
		"check authentication with existing credentials rather than asking for new credentials",
	)

	Cmd.Flags().BoolVar(
		&showCreds,
		"show-creds",
		false,
		"print credentials to stdout (useful for debugging)",
	)
}
