# Akita Plugin Interface

Plugins allow third-party developers to dynamically add functionality to the
Akita CLI without recompiling. This is achieved through the use of shared
libraries that are loaded at runtime.

## How to Implement a Plugin

All plugins implement the `AkitaPlugin` interface, which allows the plugin to
manipulate API traffic intercepted by the CLI using Akita IR format. The result
is then uploaded to Akita Cloud to analysis.

Each plugin package must also export an `LoadAkitaPlugin` function that let's
the CLI load the plugin.

To build and release the plugin, follow instructions in
[go's plugin package](https://golang.org/pkg/plugin/)

## How to Use Plugin

User can specify list of plugins on the command line as paths to shared
libraries. For example:

```
akita learn --plugins /usr/local/lib/my_plugin.so
```
