package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CLICommandPluginType types.PluginType = "cliCommand"

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(CLICommandPluginType)))
}

// CapabilitySpec advertises which CLI commands this plugin provides.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	Type              runtime.Type  `json:"type"`
	SupportedCommands []CommandSpec `json:"supportedCommands"`
}

// CommandSpec is the static metadata for a single CLI command provided by a plugin.
// It is declared once at startup and used by the CLI to build Cobra subcommands.
type CommandSpec struct {
	// Verb is the top-level action, e.g. "publish". Maps to a Cobra command group.
	Verb string `json:"verb"`
	// ObjectType is the subject, e.g. "rbsc". Becomes the subcommand under Verb.
	ObjectType string `json:"objectType"`
	Short      string `json:"short"`
	Long       string `json:"long,omitempty"`
	// Flags declares the flags this command accepts so the CLI can register them properly.
	Flags []FlagSpec `json:"flags,omitempty"`
}

// FlagSpec describes a single CLI flag a command accepts.
type FlagSpec struct {
	Name         string `json:"name"`
	Shorthand    string `json:"shorthand,omitempty"`
	Usage        string `json:"usage"`
	DefaultValue string `json:"defaultValue,omitempty"`
	// Type is one of: "string", "bool", "int", "stringSlice". Defaults to "string".
	Type string `json:"type,omitempty"`
}
