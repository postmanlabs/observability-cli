package man

var apispecPage = `
# === apispec ===

# Description

Upload traces to the Akita Cloud or use traces already stored on Akita Cloud to generate your OpenAPI3 specification.

# Examples

## akita apispec --project my-project --traces ./mytrace.har

Generates a spec from a local trace file and outputs it to stdout.

## akita apispec --project my-project --traces ./trace1.har --traces akita://my-project:trace:trace2

Generates a spec from a combination of local trace file and trace file on Akita cloud.

# Required Flags

## --traces []location

The locations to read traces from. Can be a mix of AkitaURI and local file paths.

When specifying a local file, Akita reads the HAR file and uploads it to the Akita cloud.

When specifying an AkitaURI, the format is "akita://{PROJECT}:trace:{NAME}", where "PROJECT" is the name of your project and "NAME" is the name of the trace on Akita Cloud.

# Optional Flags

## --out location

The location to store the spec. Can be an AkitaURI or a local file.

If unspecified, defaults to a new spec on Akita Cloud. Note that you must also set <bt>--project<bt>.

When specifying a local file, Akita writes the spec to the file. Note that you must also set <bt>--project<bt> when outputing to local file.

To specify <bt>stdout<bt>, use <bt>--out="-"<bt>.

When specifying an AkitaURI, the format is "akita://{PROJECT}:spec" or "akita://{PROJECT}:spec:{NAME}", where "PROJECT" is the name of your project and "NAME" is the name of the spec to create. A spec name will be generated if "NAME" is not provided.

## --project string

Akita cloud project to use to generate the spec. Only needed if --out is not specified or is not an AkitaURI.

## --service string

Alias for --project.  DEPRECATED, prefer --project.

## --cluster string

Akita cloud cluster to use to generate the spec (alias for 'project'). Only needed if --out is not specified or is not an AkitaURI.

## --format {yaml|json}

Output format for the OpenAPI 3 specification. Supports 'yaml' and 'json'.

Default is 'yaml'.

## --from-time string
## --to-time string

If provided, only trace events occurring in the given time range will be used to build the spec. Expected format is 'YYYY-MM-DD hh:mm:ss'. If desired, the 'hh:mm:ss' or the ':ss' can be omitted, in which case the start of the day or minute is used. The client's local time is assumed. If a given time occurs during a transition to or from daylight saving time, then one side of the transition is arbitrarily chosen.

## --tags []string

Add tags to the spec.

You may specify a comma separated list of "key=value" pairs (e.g. <bt>--tags a=b,c=d<bt>) or multiple separate flags (e.g. <bt>--tags a=b --tags c=d<bt>)

## --path-parameters []path-prefix

A path prefix is composed of components separated by "/". There are 3 types of components:

1. A concrete path value
2. A path parameter of the form <bt>{parameter_name}<bt>
3. A placeholder <bt>^<bt> to indicate that the component must retain the value in the trace verbatim and NOT get generalized. It behaves like a wildcard when matching path prefixes to paths.

Paths in the trace that match this path prefix are updated to use path parameters and respect placeholders that are specified.

If a path matches multiple prefixes, Akita selects the longest matching prefix.

Example 1: Simple prefix match

<bt>--path-parameters="/v1/{my_param}"<bt>

|Akita inferred endpoint|Post-processed endpoint|
|---|---|
|/v1/foo|/v1/{my_param}|
|/v1/x/y|/v1/{my_param}/y|
|/v1/{arg2}/z|/v1/{my_param}/z|

Example 2: Longest prefix match

<bt>--path-parameters="/v1/{my_param},/v1/{my_param}/123/{other_param}"<bt>

|Akita inferred endpoint|Post-processed endpoint|
|---|---|
|/v1/foo|/v1/{my_param}|
|/v1/x/123/abc|/v1/{my_param}/123/{other_param}|
|/v1/x/456/def|/v1/{my_param}/456/def|

Example 3: Akita inferred path retained

<bt>--path-parameters="/v1/foo/{param}/bar"<bt>

|Akita inferred endpoint|Post-processed endpoint|
|---|---|
|/v1/foo/x|/v1/foo/x|
|/v1/foo/baz/bar|/v1/foo/{param}/bar|
|/v1/xyz/baz/bar|/v1/xyz/baz/bar|
|/v1/{arg2}/x/bar|/v1/{arg2}/x/bar|

In this example, the endpoint /v1/{arg2}/x/bar will remain if the trace contains requests that match that endpoint with concrete path arguments in the second position that are not "foo", e.g. /v1/123/x/bar. To force the removal of the path parameter, use the placeholder ("^") component.

Example 4: Placeholder component

<bt>--path-parameters="/v1/^/{param}/bar"<bt>

|Akita inferred endpoint|Post-processed endpoint|
|---|---|
|/v1/foo/x|/v1/foo/x|
|/v1/foo/baz/bar|/v1/foo/{param}/bar|
|/v1/xyz/baz/bar|/v1/xyz/{param}/bar|
|/v1/{arg2}/x/bar|/v1/123/{param}/bar|

## --path-exclusions []string

Removes HTTP paths matching regular expressions.

For example, to filter out requests fetching files with png or jpg extensions, you can specify <bt>--path-exclusions ".*\.png" --path-exclusions ".*\.jpg"<bt>

## --infer-field-relations bool

If true, enables analysis to determine related fields in your API.

## --include-trackers bool

By default, Akita automatically filters out requests to common third-party trackers in the trace.

Set this flag to true to include them.

# GitHub Integration Flags

The following flags are needed to enable GitHub integration.

## --github-branch string

Name of github branch that this spec belongs to.

## --github-commit string

SHA of github commit that this spec belongs to.

## --github-pr int

GitHub PR number that this spec belongs to.

## --github-repo string

GitHub repo name of the form <repo_owner>/<repo_name> that this spec belongs to.

# GitLab Integration Flags

## --gitlab-mr string

GitLab merge request IID (note not ID).

For more detail on IID vs ID, see https://docs.gitlab.com/ee/api/#id-vs-iid

## --gitlab-project string

Gitlab project ID or URL-encoded path.

For more detail, see https://docs.gitlab.com/ee/api/README.html#namespaced-path-encoding

## --gitlab-branch string

Name of gitlab branch that this spec belongs to.

## --gitlab-commit string

SHA of gitlab commit that this spec belongs to.
`
