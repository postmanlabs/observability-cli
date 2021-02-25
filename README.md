# Catch breaking changes faster

Akita builds models of your APIs to help you:
* Catch breaking changes on every pull request, including added/removed endpoints, added/removed fields, modified types, modified data types
* Check compliance with intended behavior
* Auto-generate up-to-date API specs

In addition to recording traffic, Akita provides:
* Path generalization for endpoints
* Type and data format inference ([docs](https://docs.akita.software/docs/data-formats))
* Diffing across generated and uploaded API specs ([docs](https://docs.akita.software/docs/diff-api-specifications))
* Integrations with CI ([docs](https://docs.akita.software/docs/install-in-cicd)) and source control ([GitHub](https://docs.akita.software/docs/connect-to-github); [GitLab](https://docs.akita.software/docs/integrate-with-gitlab))
* Integrations with web frameworks to watch integration tests ([docs](https://docs.akita.software/docs/integrate-with-integration-tests))

See the full Akita docs [here](https://docs.akita.software/docs/welcome). Watch the first 5 minutes of [this video](https://www.youtube.com/watch?app=desktop&v=1jII0y0SGxs&ab_channel=Work-Bench) for a demo.

Sign up for our private beta [here](https://www.akitasoftware.com/get-invite).

[About this repo](#about-this-repo) | [Running this repo](#running-this-repo) | [Plugins](#plugins) | [Getting involved](#getting-involved) | [Related links](#related-links)

## About this repo
This is the open-source repository for our CLI, containing the code for:
* `apidump` for listening to API traffic and generating HAR files
* `apispec` for generating API specs from HAR files
* `apidiff` for diffing API specs

The CLI is intended for use with the Akita SaaS tool. This repository does not contain our path generalization, type and data format, or spec generation implementations.


## Running this repo

### How to build
1. Install [Go 1.15 or above](https://golang.org/doc/install). 
2. Install `libpcap`
    - For Homebrew on mac: `brew install libpcap`
    - For Ubuntu/Debian: `apt-get install libpcap-dev`
4. `go build .`

### How to test

`go test ./...`

### How to use

If it's your first use, start by logging into Akita:

```
> akita login
API Key ID: apk_0000000000000000000000
API Key Secret: ******************************
Login successful!
API keys stored in ${HOME}/.akita/credentials.yaml
```

You're now ready to run the Akita `learn` command to generate an API spec by watching traffic:

```
akita learn --service {service name} \
    --interface {network interface} \
    --port {port}
```

You should now see output that looks like this:

```
[INFO] Running learn mode on interface lo0
[INFO] Preview the learned API spec at https://app.akita.software/service/svc_4UxRRNmhL8R2MAqrmCmCvn/spec-preview/lrn_40lxi9tlHnfdiRsBzXYbBY
[INFO] Send SIGINT (Ctrl-C) to stop...
```

For more information, see the docs: [QuickStart: Server APIs](https://docs.akita.software/docs/get-started-with-superlearn); [`apidump`](https://docs.akita.software/docs/apidump); [`apispec`](https://docs.akita.software/docs/apispec-1); [`learn`](https://docs.akita.software/docs/learn); [`apidiff`](https://docs.akita.software/docs/apidiff).

## Plugins

Client-side inference for the Akita CLI happens through our plugins: for instance, API path argument generalization and type and data format inference. Please refer to [README in plugin](plugin/README.md) for more information.

If you want to contribute to this repository, we recommend submitting pull requests directly rather than developing plugins, as it makes distribution easier.

## Getting involved
* Please file bugs as issue to this repository.
* We welcome contributions! If you want to make changes or build your own extensions to the CLI on top of the [Akita IR](https://github.com/akitasoftware/akita-ir), please see our [CONTRIBUTING](CONTRIBUTING.md) doc.
* We're always happy to answer any questions about the CLI, or about how you can contribute. Email us at `opensource [at] akitasoftware [dot] com` and/or [request to join our Slack](https://docs.google.com/forms/d/e/1FAIpQLSfF-Mf4Li_DqysCHy042IBfvtpUDHGYrV6DOHZlJcQV8OIlAA/viewform?usp=sf_link)!

## Related links
Using the Akita beta:
* [Akita docs](https://docs.akita.software/docs/welcome)
* [Watch a demo](https://www.youtube.com/watch?app=desktop&v=1jII0y0SGxs&ab_channel=Work-Bench) (first ~5 min)
* [Sign up for our private beta](https://www.akitasoftware.com/get-invite)

The Akita philosophy:
* [On CloudBees's DevOps Radio](https://www.cloudbees.com/resources/devops-radio/jean-yang)
* [On Mulesoft's APIs Unplugged](https://soundcloud.com/mulesoft/apis-unplugged-season-2-episode-3-understanding-systems-through-apis-with-dr-jean-yang)
* [At the API Specs Conference](https://www.youtube.com/watch?v=uYA4DsuMrg8)
* [At Rebase](https://2020.splashcon.org/details/splash-2020-rebase/4/APIs-are-Illness-and-Cure-The-Software-Heterogeneity-Problem-in-Web-Programming)

