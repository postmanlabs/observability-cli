package akiflag

// A set of variables holding the values of global flags exposed by the root
// command. This allows us to share those values with subcommands without
// creating an import loop.

var Domain string
