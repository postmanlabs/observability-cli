package man

var apidumpPage = `
# === apidump ===

# Description

Capture and store a sequence of requests/responses to a service by observing
network traffic.

# Examples

## akita apidump --filter "port 80" --out mytracedir

Capture requests/responses going to or coming from port 80 and store them into a directory called "mytracedir".

## akita apidump --filter "port 80" --out akita://my-service:trace:mytrace

Capture requests/responses going to or coming from port 80 and store them into a trace on the Akita cloud called "mytrace".

## akita apidump --filter "port 80" --out akita://my-service:trace

Capture requests/responses going to or coming from port 80 and store them into a trace on the Akita cloud with a generated name.

## akita apidump --filter "port 80" -c ./my_tests.sh -u ${USER}

Run <bt>my_tests.sh<bt> as <bt>${USER}<bt> and capture requests/responses going to or coming from port 80. Akita will automatically terminate once the script finishes.

# Optional Flags

## --out location

The location to store the trace. Can be an AkitaURI or a local directory.

If not specified, defaults to a trace on Akita Cloud. Note that you must supply <bt>--service<bt> in this case.

When specifying a local directory, Akita writes HAR files to the directory.

When specifying an AkitaURI, the format is "akita://{SERVICE}:trace" or "akita://{SERVICE}:trace:{NAME}", where "SERVICE" is the name of your service and "NAME" is the name of the trace on Akita Cloud where the collected data is stored. A trace name will be generated if "NAME" is not provided.

Exactly one of <bt>--out<bt> or <bt>--service<bt> must be provided.

## --service string

Your Akita service. Exactly one of <bt>--out<bt> or <bt>--service<bt> must be provided.

## --filter string

Used to match packets going to and coming from your API service.

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
`
