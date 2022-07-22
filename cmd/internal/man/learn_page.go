package man

var learnPage = `
# === learn ===

# Description

Generate API specifications from network traffic.

# Examples

## akita learn --filter "port 80" --project my-project

Capture requests/responses going to or coming from port 80 and convert them into an API spec.

## akita learn --filter "port 80" -c ./my_tests.sh -u ${USER}

Run <bt>my_tests.sh<bt> as <bt>${USER}<bt> and capture requests/responses going to or coming from port 80. Akita will automatically terminate once the script finishes.

# Optional Flags

## --out location

The location to store the spec. Can be an AkitaURI or a local file.

If not specified, defaults to a trace on Akita Cloud. Note that you must supply <bt>--project<bt> in this case.

When specifying an AkitaURI, the format is "akita://{PROJECT}:spec" or "akita://{PROJECT}:spec:{NAME}", where "PROJECT" is the name of your project and "NAME" is the name of the spec on Akita Cloud where the collected data is stored. A spec name will be generated if "NAME" is not provided.

## --project string

Your Akita project. Only needed if <bt>--out<bt> is not an AkitaURI.

## --service string

Alias for --project.  DEPRECATED, prefer --project.

## --cluster string

Your Akita cluster (alias for 'project'). Only needed if <bt>--out<bt> is not an AkitaURI.

## --filter string

Used to match packets going to and coming from your API project.

For example, to match packets destined/originated from port 80, you would set <bt>--filter="port 80"<bt>.

The syntax follows BPF syntax (man 7 pcap-filter).

This filter is applied uniformly across all network interfaces, as set by <bt>--interfaces<bt> flag.

## --interfaces []string

List of network interfaces to listen on (e.g. "lo" or "eth0").

You may specify a comma separated string (e.g. --interfaces lo,eth0) or multiple separate flags (e.g. --interfaces lo --interfaces eth0).

If not set, defaults to all interfaces on the host.

## --sample-rate number

A number between [0.0, 1.0] to control sampling.

## --tags []string

Adds tags to the dump.

You may specify a comma separated list of "key=value" pairs (e.g. <bt>--tags a=b,c=d<bt>) or multiple separate flags (e.g. <bt>--tags a=b --tags c=d<bt>)

## --command, -c string

A command that generates requests and responses for Akita to observe. Akita will execute the command (similar to <bt>bash -c<bt>) and automatically terminate when the command finishes, without needing to receive a SIGINT.

By default, the command runs as the current user. As a safety precaution, if the current user is <bt>root<bt>, you must use the <bt>-u<bt> flag to explicitly indicate that you want to run as <bt>root<bt>.

## --user, -u string

Username of the user to use when running the command specified in <bt>-c<bt>

## --path-exclusions []string

Removes HTTP paths matching regular expressions.

For example, to filter out requests fetching files with png or jpg extensions, you can specify <bt>--path-exclusions ".*\.png" --path-exclusions ".*\.jpg"<bt>

## --host-exclusions []string

Removes HTTP hosts matching regular expressions.

For example, to filter out requests to all subdomains of <bt>example.com<bt>, you can specify <bt>--host-exclusions ".*example.com"<bt>

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
