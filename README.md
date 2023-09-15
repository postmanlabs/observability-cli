# Welcome! 👋

Our team at Postman is working towards the open launch of Live Insights. Today,
the alpha launch focuses on the Live Collections Agent (LCA), which passively
watches your API traffic to automatically populate a Postman Collection with
API endpoints. Within 15 minutes of installing the Live Collections Agent in
staging or production, you’ll start to see endpoints in your collection. The
Live Collections Agent will keep these endpoints up to date based on new
observed traffic.

API discovery is just the beginning of what we plan to offer. Akita users are
familiar with the ability to explore API models based on error, latency, and
volume.

We’re hoping you will try out the new features and give us your feedback to
help us continue tailoring the product to your needs.

  [About this repo](#about-this-repo)
| [Running this repo](#running-this-repo)

## About this repo
This is the open-source repository for the community version of the LCA, and is
intended for use with Postman. This community version of the LCA does not
include functionality for inferring types and data formats. This functionality
is available only in the `postman-lc-agent` binary that we distribute.

## Running this repo

### How to build
Running the following commands will generate the `postman-lc-agent` binary:
1. Install [Go 1.18 or above](https://golang.org/doc/install).
2. Install `libpcap`
    - For Homebrew on mac: `brew install libpcap`
    - For Ubuntu/Debian: `apt-get install libpcap-dev`
3. `make`


### How to test

1. Install [gomock](https://github.com/golang/mock): `go get github.com/golang/mock/mockgen`
2. `make test`
