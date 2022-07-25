package man

var setversionPage = `
# === setversion ===

# Description

Sets the version name of an API spec. Version names are unique within a project. For example, "latest" is a version name automatically assigned to the most recent API spec.

# Examples

## akita setversion v1.0.0 akita://my-project:spec:beta

Marks the spec identified by <bt>akita://my-project:spec:beta<bt> as "v1.0.0" for <bt>my-project<bt>. Any spec in <bt>my-project<bt> that was previously marked with "v1.0.0" will no longer be associated with that version name.

## akita setversion stable akita://my-project:spec:public

Marks the spec identified by <bt>akita://my-project:spec:public<bt> as "stable" for <bt>my-project<bt>. This removes the "stable" designation from all other specs in <bt>my-project<bt>.
`
