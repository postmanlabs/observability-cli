package man

var apidiffPage = `
# === apidiff ===

# Description

Compare 2 API specs.

# Examples

## akita apidiff akita://my-service:spec:spec1 akita://my-service:spec:spec2

# Optional Flags

## --out location

The location to store the diff as JSON. Can be a file or "-" for stdout.

If not specified, defaults to an interactive mode for exploring the diff on the command line.
`
