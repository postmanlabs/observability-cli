package apidiff

var (
	// Optional flags
	outFlag string
)

func init() {
	//
	// Optional Flags
	//
	Cmd.Flags().StringVar(
		&outFlag,
		"out",
		"",
		"Where to store the diff. If unset, the diff is presented interactively. Use '-' for stdout.")
}
