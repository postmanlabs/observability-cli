package man

import "github.com/akitasoftware/akita-cli/cmd/internal/upload"

var uploadPage = `
# === upload ===

# Description

Uploads an OpenAPI 3 spec or a HAR file to Akita Cloud. When the upload completes, the command prints the uploaded object's Akita URI to stdout.

# Examples

## akita upload --dest akita://my-project:spec /path/to/spec.yaml

Upload the given file as a spec for my-project. A new name is generated for the spec.

## akita upload --dest akita://my-project:spec:spec1 /path/to/spec.yaml

Upload the given file as a spec for my-project. The uploaded spec will be called "spec1".

## akita upload --dest akita://my-project:trace /path/to/trace1.har /path/to/trace2.har

Upload the given files as a trace for my-project. A new name is generated for the trace.

## akita upload --dest akita://my-project:trace:trace1 /path/to/trace1.har /path/to/trace2.har

Upload the given files as a trace for my-project. The uploaded trace will be called "trace1".

# Required Flags

## --dest akita_uri

The Akita URI where you want to upload to, specifying the Akita project, the type of the upload, and optionally, the name of the upload.

If a name is provided and an object already exists with that name, an error occurs, unless '--append' is also specified.

# Optional Flags

## --append

Add the upload to an existing Akita trace. If a trace with the given name doesn't already exist, it will be created and a warning will be printed to stderr. Only relevant to trace uploads.

## --include-trackers

By default, Akita automatically filters out requests to common third-party trackers in the trace. Use this flag to include them. Only relevant to trace uploads.

## --timeout duration

Timeout for transferring and processing the upload. Defaults to ` + upload.DEFAULT_TIMEOUT.String() + `.
`
