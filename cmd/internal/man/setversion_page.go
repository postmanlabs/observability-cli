package man

var setversionPage = `
# === setversion ===

# Description

Sets the version name of an API spec. Version names are unique within a service. For example, "latest" is a version name automatically assigned to the most recent API spec.

# Examples

## akita setversion v1.0.0 akita://my-service:spec:beta

Marks the spec identified by <bt>akita://myservice:spec:beta<bt> as "v1.0.0" for <bt>my-service<bt>. Any spec in <bt>my-service<bt> that was previously marked with "v1.0.0" will no longer be associated with that version name.

## akita setversion stable akita://my-service:spec:public

Marks the spec identified by <bt>akita://myservice:spec:public<bt> as "stable" for <bt>my-service<bt>. This removes the "stable" designation from all other specs in <bt>my-service<bt>.
`
