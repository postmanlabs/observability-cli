package man

var uploadPage = `
# === upload ===

# Description

Uploads an OpenAPI 3 spec to Akita Cloud.

Once uploaded, the command prints the uploaded spec's Akita URI to stdout.

# Examples

## akita upload --service my-service /path/to/spec.yaml

# Required Flags

## --service string

The Akita service where you want to upload this spec to.

# Optional Flags

## --name string

A unique name to assign to the uploaded spec.

## --timeout duration

Timeout for uploading and processing the spec.
`
