// Package index is the compiled-in list of binaries p3bot manages and the
// module each is built from. Package paths are deliberately absent: they
// change between versions and come from each version's manifest.
//
// The managed tool table in AGENTS.md mirrors this list, and a test holds
// the two together.
package index

import (
	"slices"
)

// Tool is a managed binary and the module that builds it. Binaries sharing
// a module always run the same version.
type Tool struct {
	Name   string
	Module string
}

var tools = []Tool{
	{"start", "github.com/p3bot/start"},
	{"agentdex", "github.com/p3bot/agentdex"},
	{"tk", "github.com/p3bot/tk"},
	{"tkv", "github.com/p3bot/tk"},
	{"kagi", "github.com/p3bot/kagi"},
	{"snag", "github.com/p3bot/snag"},
	{"webctl", "github.com/p3bot/webctl"},
	{"hotseat", "github.com/p3bot/hotseat"},
	{"p3bot", "github.com/p3bot/p3bot"},
}

// Tools returns every managed binary in display order.
func Tools() []Tool { return slices.Clone(tools) }

// Names returns every managed binary name in display order.
func Names() []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// Lookup returns the managed binary called name.
func Lookup(name string) (Tool, bool) {
	i := slices.IndexFunc(tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		return Tool{}, false
	}
	return tools[i], true
}
