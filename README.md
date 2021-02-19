# Catch breaking changes faster

Akita builds models of your APIs to help you:
* Catch breaking changes on every pull request, including added/removed endpoints, added/removed fields, modified types, modified data types
* Check compliance with intended behavior
* Auto-generate up-to-date API specs

In addition to recording traffic, Akita provides:
* Path generalization for endpoints
* Type and data format inference ([docs](https://docs.akita.software/docs/data-formats))
* Integrations with CI ([docs](https://docs.akita.software/docs/install-in-cicd)) and source control ([GitHub](https://docs.akita.software/docs/connect-to-github); [GitLab](https://docs.akita.software/docs/integrate-with-gitlab))
* Integrations with web frameworks to watch integration tests ([docs](https://docs.akita.software/docs/integrate-with-integration-tests))

See the full Akita docs [here](https://docs.akita.software/docs/welcome). Watch the first 5 minutes of [this video](https://www.youtube.com/watch?app=desktop&v=1jII0y0SGxs&ab_channel=Work-Bench) for a demo.

Sign up for our private beta [here](https://www.akitasoftware.com/get-invite).

---

This is the open-source repository for our CLI, containing the code for:
* `apidump` for listening to API traffic and generating HAR files
* `apispec` for generating API specs from HAR files
* `apidiff` for diffing API specs
The CLI is intended for use with the Akita SaaS tool.
