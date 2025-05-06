// Package types contains the following types:
//   - **Config**: The plugin configuration including the connection types for a plugin.
//   - **Plugin**: The struct that is used to track discovered plugins. This struct is shared amongst the registries who also track
//     them to certain extent.
//   - **Types**: The struct that will be Marshalled and sent back to plugin manager. This struct is used to track the
//     types a plugin supports. Plural because, for example, a Transformation plugin would support at least two. The
//     incoming type and the output type. The format is PluginType/list of types.
package types
