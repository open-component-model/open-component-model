// Package plugins provides various utilities and helpers for interacting with plugins,
// including sending HTTP requests to plugin endpoints, decoding request bodies, and validating
// plugin types against JSON schemas.
//
// The package also includes mechanisms for managing plugin error handling, waiting for plugin
// readiness, and making plugin calls with various options.
//
// Key components:
//   - **Call**: Makes HTTP requests to plugin endpoints with optional payloads, headers, and query parameters.
//   - **Error**: A custom error type for handling plugin-related errors in HTTP responses.
//   - **ValidatePlugin**: Validates an incoming raw type against a given JSON schema.
//   - **WaitForPlugin**: Waits for a plugin to become ready by making periodic health checks. Once the plugin is ready
//     it sets up a client which can then be used to interact with said plugin.
package plugins
