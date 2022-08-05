# Welcome! 👋

Drop in Akita for instant API observability.

Powered by eBPF and state-of-the-art API traffic modeling algorithms, Akita
enables any developer to understand and track API endpoints and their usage in
real time:

  * **Understand what API endpoints you have.** Get a continuously up-to-date,
    searchable map of your API endpoints, fields, data types, and more. Export
    as OpenAPI specs.

  * **Understand API usage.** Get an automatically updated, per-endpoint view
    of volume, latency, and errors—without having to make code changes or build
    your own dashboards.

  * **Automatically understand API behavior changes.** Quickly get a summary of
    changes to performance and errors, added and removed endpoints, and more.

Simply drop Akita into your system to understand your system behavior, without
having to use a proxy or instrument code service by service.

We're in beta and would love to have you try us out!
[Join here](https://www.akitasoftware.com/beta-signup).

  [About this repo](#about-this-repo)
| [Running this repo](#running-this-repo)
| [Getting involved](#getting-involved)
| [Related links](#related-links)

## About this repo
This is the open-source repository for the community version of our CLI, and is
intended for use with the Akita console. This community version of the CLI does
not include functionality for inferring types and data formats. This
functionality is available only in the `akita` binary that we distribute.

## Running this repo

### How to build
Running the following commands will generate the `akita-cli` binary:
1. Install [Go 1.18 or above](https://golang.org/doc/install). 
2. Install `libpcap`
    - For Homebrew on mac: `brew install libpcap`
    - For Ubuntu/Debian: `apt-get install libpcap-dev`
3. `make`


### How to test

1. Install [gomock](https://github.com/golang/mock): `go get github.com/golang/mock/mockgen`
2. `make test`

### How to use

See our docs: [Single Host/VM](https://docs.akita.software/docs/run-locally).

## Getting involved
* Please file bugs as issues to this repository.
* We welcome contributions! If you want to make changes or build your own
  extensions to the CLI on top of the
  [Akita IR](https://github.com/akitasoftware/akita-ir), please see our
  [CONTRIBUTING](CONTRIBUTING.md) doc.
* We're always happy to answer any questions about the CLI, or about how you
  can contribute. Email us at `opensource [at] akitasoftware [dot] com` or
  [request to join our Slack](https://docs.google.com/forms/d/e/1FAIpQLSfF-Mf4Li_DqysCHy042IBfvtpUDHGYrV6DOHZlJcQV8OIlAA/viewform?usp=sf_link)!

## Related links
* [Akita blog](https://www.akitasoftware.com/blog)
* [Akita docs](https://docs.akita.software/)
* [Sign up for our private beta](https://www.akitasoftware.com/beta-signup)
