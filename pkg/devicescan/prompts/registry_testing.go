package prompts

import "testing"

// SetForTest replaces the global scanner registry with the given
// scanners for the duration of t, restoring it on cleanup. Test-only
// helper so callers in other packages don't reach into registry
// internals.
func SetForTest(t *testing.T, scanners ...PromptScanner) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = append([]PromptScanner(nil), scanners...)
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}
