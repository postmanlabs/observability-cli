# Akita Plugins

Akita plugins allow developers to add additional functionality to the packet captures 
performed by the Akita CLI.  Possible examples include:
  * adding extra annotations such as data formats
  * filtering or obfuscating sensitive information not covered by Akita's existing obfuscation
  * rewriting endpoint names

Currently, we use plugins for the Akita binary that we release, for the proprietary parts of the 
Akita CLI that are not open-source.

## How to Implement a Plugin

All plugins implement the `AkitaPlugin` interface, which allows the plugin to
manipulate API traffic intercepted by the CLI using Akita IR format. The result
is then uploaded to Akita Cloud to analysis.

Each plugin package must export an `LoadAkitaPlugin` function that lets
the CLI load the plugin, and gives the plugin a chance to report initialization failures.

Plugins currently cannot accept command-line options; any necessary configuration
can be passed in as an environment variable.

## Compiled Plugins

The preferred method of implementing a plugin is to compile it into the `akita` program
itself, to avoid problems with dynamic linking.

### Development and Testing

Create your plugin in a new source repository, and set it up as a Go module.

In `cmd/internal/pluginloader/precompiled_plugins.go`, add your module to the imports list, and
assign the plugin a name in the `PrecompiledPlugins` map.

Use `go get` to retrieve a particular version of your plugin (the latest by default.)

To test locally, you can add a `replace` directive in `akita-cli`'s `go.mod` file to refer
to the checked-out source of your plugin, instead of retrieving it from the repository.  For example

```
replace github.com/example/my-plugin => /home/me/repos/my-plugin-dev
```

### Example Plugin

https://github.com/mgritter/sample-akita-plugin contains an example Akita plugin.

Your module will have `akita-cli`, `akita-ir`, and possibly `akita-libs` as dependencies. Unfortunately,
the CLI depends on extension of `google/martian`, so your `go.mod` will need the following in order to
comple locally:

```
replace github.com/google/martian/v3 v3.0.1 => github.com/akitasoftware/martian/v3 v3.0.1-0.20210608174341-829c1134e9de
```

### Release

Submit a PR to `akitasoftware/akita-cli` containing your changes to the `precompiled_plugins.go` file.
The next CLI release following the acceptance of your PR will have that particular version.

The version specified in `akita-cli`'s `go.mod` is the one that will be compiled. A new PR updating the
import will be needed to upgrade the released version of the plugin.

## Dynamically loaded Plugins (EXPERIMENTAL)

To build and release the plugin, follow instructions in
[go's plugin package](https://golang.org/pkg/plugin/)

Our experience to date is that this will only work if you compile the plugin
so that its dependencies refer to the exact same source paths as where `akita-cli` is built.
This means it is nearly impossible to get it to work with an official Akita release.
See https://github.com/golang/go/issues/31354

Here is a sample `go.mod` for compiling a dynamically loaded plugin.  If you compile `akita-cli` from source, and then
on the same system compile your plugin with a replace directive like that below, then the plugin can be dynamically loaded.


```
module example.com/my-plugin

go 1.16

require (
	github.com/akitasoftware/akita-cli v0.0.0-20210819211557-bffed150667b
	github.com/akitasoftware/akita-ir v0.0.0-20210818150446-55531f1ef499
)

# This is an Akita extension of the Martian library for handling HAR files.
replace github.com/google/martian/v3 v3.0.1 => github.com/akitasoftware/martian/v3 v3.0.1-0.20210608174341-829c1134e9de

# Replace the module with the path you use to locally compile akita-cli
replace github.com/akitasoftware/akita-cli => ../akita-cli

```

Go's plugin runtime will return an error stating "plugin was built with a different version of package akita-cli" even if the versions match,
if the package hashes do not also match-- because the packages were compiled in different locations.  So an alternate workaround is to
compile `akita-cli` in `$GOPATH/pkg/mod/github.com/akitasoftware/akita-cli@v...`.

## How to Use Plugins

User can specify list of plugins on the command line as paths to shared
libraries, or the name of a precompiled plugin. For example:

```
akita learn --plugins /usr/local/lib/my_plugin.so
```

The plugins will be executed in the order given on the command line.
