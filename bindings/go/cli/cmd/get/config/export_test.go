package config

// GetEffectiveConfig exposes getEffectiveConfig to the external test package, which
// needs the command test helper and so cannot live inside this package.
var GetEffectiveConfig = getEffectiveConfig
