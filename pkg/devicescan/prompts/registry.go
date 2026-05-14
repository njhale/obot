package prompts

import "sync"

var (
	registryMu sync.RWMutex
	registry   []PromptScanner
)

// Register adds a PromptScanner to the global registry. Intended to be
// called from a scanner sub-package's init() — e.g.
//
//	func init() { prompts.Register(&claudeCodeScanner{}) }
//
// Duplicate Client() identifiers are allowed but the registry preserves
// registration order; the CLI dedupes per-client at the top-level
// TopK step.
func Register(s PromptScanner) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, s)
}

// All returns a snapshot of the registered scanners in registration
// order. Mutating the returned slice does not affect the registry.
func All() []PromptScanner {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]PromptScanner, len(registry))
	copy(out, registry)
	return out
}
