package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/mcp"
	"github.com/obot-platform/obot/pkg/storage"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeleteCatalogEntryCompositeDependencies(t *testing.T) {
	objects := catalogEntryDeletionObjects()
	storage := newFakeStorage(t, objects...)
	req := httptest.NewRequest(http.MethodDelete, "/api/mcp-catalogs/default/entries/source", nil)
	req.SetPathValue("catalog_id", system.DefaultCatalog)
	req.SetPathValue("entry_id", "source")
	recorder := httptest.NewRecorder()

	err := (&MCPCatalogHandler{}).DeleteEntry(api.Context{
		ResponseWriter: recorder,
		Request:        req,
		Storage:        storage,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, recorder.Code)

	var conflict types.MCPCompositeDeletionConflict
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &conflict))
	assert.Equal(t, "MCP catalog entry is used by composite catalog entries", conflict.Message)
	assert.Equal(t, []types.MCPCompositeDeletionDependency{
		{Name: "Composite", CatalogEntryID: "composite"},
		{Name: "Runtime Composite", MCPServerID: "runtime-composite", CatalogEntryID: "composite"},
		{Name: "Single Component", CatalogEntryID: "single-component", WillBeDeleted: true},
	}, conflict.Dependencies)

	for _, name := range []string{"source", "composite", "single-component"} {
		var entry v1.MCPServerCatalogEntry
		require.NoError(t, storage.Get(t.Context(), client.ObjectKey{Namespace: system.DefaultNamespace, Name: name}, &entry))
	}
}

func TestForceDeleteCatalogEntryCascadesToCatalogComposites(t *testing.T) {
	objects := catalogEntryDeletionObjects()
	storage := newFakeStorage(t, objects...)
	req := httptest.NewRequest(http.MethodDelete, "/api/mcp-catalogs/default/entries/source?force=true", nil)
	req.SetPathValue("catalog_id", system.DefaultCatalog)
	req.SetPathValue("entry_id", "source")

	err := (&MCPCatalogHandler{}).DeleteEntry(api.Context{
		ResponseWriter: httptest.NewRecorder(),
		Request:        req,
		Storage:        storage,
	})
	require.NoError(t, err)

	for _, name := range []string{"source", "single-component"} {
		var entry v1.MCPServerCatalogEntry
		err := storage.Get(t.Context(), client.ObjectKey{Namespace: system.DefaultNamespace, Name: name}, &entry)
		assert.True(t, apierrors.IsNotFound(err), "expected %s to be deleted, got %v", name, err)
	}

	var composite v1.MCPServerCatalogEntry
	require.NoError(t, storage.Get(t.Context(), client.ObjectKey{Namespace: system.DefaultNamespace, Name: "composite"}, &composite))
	require.Len(t, composite.Spec.Manifest.CompositeConfig.ComponentServers, 1)
	assert.Equal(t, "other", composite.Spec.Manifest.CompositeConfig.ComponentServers[0].CatalogEntryID)
	assert.Empty(t, composite.Spec.Manifest.CompositeConfig.ComponentServers[0].Manifest)
	assert.Nil(t, composite.Spec.Manifest.ToolPreview)

	// Existing runtimes preserve their snapshots and drift until the user or
	// admin upgrades the runtime composite.
	var runtimeComposite v1.MCPServer
	require.NoError(t, storage.Get(t.Context(), client.ObjectKey{Namespace: system.DefaultNamespace, Name: "runtime-composite"}, &runtimeComposite))
	require.Len(t, runtimeComposite.Spec.Manifest.CompositeConfig.ComponentServers, 2)
	assert.Equal(t, "source", runtimeComposite.Spec.Manifest.CompositeConfig.ComponentServers[0].CatalogEntryID)
}

func catalogEntryDeletionObjects() []client.Object {
	metadata := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: system.DefaultNamespace}
	}
	component := func(name string) types.CatalogComponentServer {
		return types.CatalogComponentServer{
			CatalogEntryID: name,
			Manifest:       types.MCPServerCatalogEntryManifest{Name: "stale " + name},
		}
	}
	return []client.Object{
		&v1.MCPCatalog{ObjectMeta: metadata(system.DefaultCatalog)},
		&v1.MCPServerCatalogEntry{
			ObjectMeta: metadata("source"),
			Spec: v1.MCPServerCatalogEntrySpec{
				Editable:       true,
				MCPCatalogName: system.DefaultCatalog,
				Manifest:       types.MCPServerCatalogEntryManifest{Name: "Source", Runtime: types.RuntimeRemote},
			},
		},
		&v1.MCPServerCatalogEntry{
			ObjectMeta: metadata("other"),
			Spec: v1.MCPServerCatalogEntrySpec{
				Editable:       true,
				MCPCatalogName: system.DefaultCatalog,
				Manifest:       types.MCPServerCatalogEntryManifest{Name: "Other", Runtime: types.RuntimeRemote},
			},
		},
		&v1.MCPServerCatalogEntry{
			ObjectMeta: metadata("composite"),
			Spec: v1.MCPServerCatalogEntrySpec{
				MCPCatalogName: system.DefaultCatalog,
				Manifest: types.MCPServerCatalogEntryManifest{
					Name:        "Composite",
					Runtime:     types.RuntimeComposite,
					ToolPreview: []types.MCPServerTool{{Name: "stale"}},
					CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
						component("source"), component("other"),
					}},
				},
			},
		},
		&v1.MCPServerCatalogEntry{
			ObjectMeta: metadata("single-component"),
			Spec: v1.MCPServerCatalogEntrySpec{
				MCPCatalogName: system.DefaultCatalog,
				Manifest: types.MCPServerCatalogEntryManifest{
					Name: "Single Component", Runtime: types.RuntimeComposite,
					CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{component("source")}},
				},
			},
		},
		&v1.MCPServer{
			ObjectMeta: metadata("runtime-composite"),
			Spec: v1.MCPServerSpec{
				MCPServerCatalogEntryName: "composite",
				Manifest: types.MCPServerManifest{
					Name: "Runtime Composite", Runtime: types.RuntimeComposite,
					CompositeConfig: &types.CompositeRuntimeConfig{ComponentServers: []types.ComponentServer{
						{CatalogEntryID: "source", Manifest: types.MCPServerManifest{Name: "Source snapshot", Runtime: types.RuntimeRemote}},
						{CatalogEntryID: "other", Manifest: types.MCPServerManifest{Name: "Other snapshot", Runtime: types.RuntimeRemote}},
					}},
				},
			},
		},
	}
}

func TestAcceptCatalogEntryOwnership(t *testing.T) {
	entry := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"example.com/keep": "true",
			},
		},
		Spec: v1.MCPServerCatalogEntrySpec{
			Editable:  false,
			Detached:  true,
			SourceURL: "https://github.com/obot-platform/mcp-catalog",
			Manifest: types.MCPServerCatalogEntryManifest{
				EntryKey: "context7",
			},
		},
	}

	acceptCatalogEntryOwnership(entry)

	assert.True(t, entry.Spec.Editable)
	assert.Empty(t, entry.Spec.SourceURL)
	assert.Empty(t, entry.Spec.Manifest.EntryKey)
	assert.False(t, entry.Spec.Detached)
	assert.Equal(t, "true", entry.Annotations["example.com/keep"])
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic spaces",
			input:    "My App Config",
			expected: "my-app-config",
		},
		{
			name:     "single quotes and spaces",
			input:    "My App's Config",
			expected: "my-app-s-config",
		},
		{
			name:     "special characters",
			input:    "Test_Server@1.0!",
			expected: "test-server-1-0",
		},
		{
			name:     "mixed case with symbols",
			input:    "Special!@#$%Characters",
			expected: "special-characters",
		},
		{
			name:     "multiple consecutive spaces",
			input:    "App   With   Spaces",
			expected: "app-with-spaces",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  App Config  ",
			expected: "app-config",
		},
		{
			name:     "leading and trailing special chars",
			input:    "!!!App Config***",
			expected: "app-config",
		},
		{
			name:     "only special characters",
			input:    "!@#$%^&*()",
			expected: "",
		},
		{
			name:     "already valid name",
			input:    "my-valid-name",
			expected: "my-valid-name",
		},
		{
			name:     "numbers and hyphens",
			input:    "app-v1.2.3",
			expected: "app-v1-2-3",
		},
		{
			name:     "unicode characters",
			input:    "café-résumé",
			expected: "caf-r-sum",
		},
		{
			name:     "long name gets truncated",
			input:    "this-is-a-very-long-name-that-exceeds-the-kubernetes-limit-of-sixty-three-characters-and-should-be-truncated",
			expected: "this-is-a-very-long-name-that-exceeds-the-kubernetes-limit-of-s",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: "",
		},
		{
			name:     "uppercase letters",
			input:    "UPPERCASE-NAME",
			expected: "uppercase-name",
		},
		{
			name:     "mixed alphanumeric with symbols",
			input:    "App123@#$Test456",
			expected: "app123-test456",
		},
		{
			name:     "parentheses and brackets",
			input:    "App (v2.0) [Production]",
			expected: "app-v2-0-production",
		},
		{
			name:     "dots and underscores",
			input:    "my.app_name.config",
			expected: "my-app-name-config",
		},
		{
			name:     "consecutive special chars become single dash",
			input:    "app!!!@@@###config",
			expected: "app-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeMCPCatalogEntryName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeNameKubernetesCompliance(t *testing.T) {
	testCases := []string{
		"My App's Config",
		"Test_Server@1.0!",
		"Special!@#$%Characters",
		"App   With   Spaces",
		"  App Config  ",
		"café-résumé",
		"UPPERCASE-NAME",
		"App (v2.0) [Production]",
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			result := normalizeMCPCatalogEntryName(input)

			// Test length constraint
			if len(result) > 63 {
				t.Errorf("NormalizeName(%q) = %q has length %d, exceeds 63 characters", input, result, len(result))
			}

			// Test character constraints (only lowercase alphanumeric and hyphens)
			for i, r := range result {
				if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
					t.Errorf("NormalizeName(%q) = %q contains invalid character %q at position %d", input, result, r, i)
				}
			}

			// Test that it doesn't start or end with hyphen (unless empty)
			if len(result) > 0 {
				if result[0] == '-' {
					t.Errorf("NormalizeName(%q) = %q starts with hyphen", input, result)
				}
				if result[len(result)-1] == '-' {
					t.Errorf("NormalizeName(%q) = %q ends with hyphen", input, result)
				}
			}
		})
	}
}

func newEntry(catalogName, workspaceID string) v1.MCPServerCatalogEntry {
	return v1.MCPServerCatalogEntry{
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName:       catalogName,
			PowerUserWorkspaceID: workspaceID,
		},
	}
}

func TestValidateEntryScope(t *testing.T) {
	tests := []struct {
		name        string
		entry       v1.MCPServerCatalogEntry
		catalogName string
		workspaceID string
		expectError bool
	}{
		{
			name:        "catalog entry matches catalog scope",
			entry:       newEntry("default", ""),
			catalogName: "default",
			expectError: false,
		},
		{
			name:        "catalog entry mismatches catalog scope",
			entry:       newEntry("default", ""),
			catalogName: "other",
			expectError: true,
		},
		{
			name:        "workspace entry matches workspace scope",
			entry:       newEntry("", "ws1"),
			workspaceID: "ws1",
			expectError: false,
		},
		{
			name:        "workspace entry mismatches workspace scope",
			entry:       newEntry("", "ws1"),
			workspaceID: "ws2",
			expectError: true,
		},
		{
			name:        "global catalog entry rejected by strict workspace check",
			entry:       newEntry("default", ""),
			workspaceID: "ws1",
			expectError: true,
		},
		{
			name:        "unscoped request for unscoped entry passes",
			entry:       newEntry("", ""),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntryScope(tt.entry, tt.catalogName, tt.workspaceID)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEntryVisibleFromScope(t *testing.T) {
	tests := []struct {
		name        string
		entry       v1.MCPServerCatalogEntry
		catalogName string
		workspaceID string
		expectError bool
	}{
		{
			name:        "catalog entry matches catalog scope",
			entry:       newEntry("default", ""),
			catalogName: "default",
			expectError: false,
		},
		{
			name:        "catalog entry mismatches catalog scope",
			entry:       newEntry("default", ""),
			catalogName: "other",
			expectError: true,
		},
		{
			name:        "workspace entry matches workspace scope",
			entry:       newEntry("", "ws1"),
			workspaceID: "ws1",
			expectError: false,
		},
		{
			name:        "workspace entry mismatches workspace scope",
			entry:       newEntry("", "ws1"),
			workspaceID: "ws2",
			expectError: true,
		},
		{
			name:        "global catalog entry allowed via workspace scope (relaxed)",
			entry:       newEntry("default", ""),
			workspaceID: "ws1",
			expectError: false,
		},
		{
			name:        "entry with no scope rejected via workspace scope",
			entry:       newEntry("", ""),
			workspaceID: "ws1",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntryVisibleFromScope(tt.entry, tt.catalogName, tt.workspaceID)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPrepareTempServerConfigDoesNotUseBoundSecretInURL(t *testing.T) {
	const (
		namespace = "obot-ns"
		label     = "allowed-secret"
		key       = "WORKSPACE"
	)
	localK8sClient := fake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-secret", Namespace: namespace, Labels: map[string]string{label: "true"}},
		Data:       map[string][]byte{"token": []byte("secret-value")},
	}).Build()
	manifest := types.MCPServerManifest{
		Runtime: types.RuntimeRemote,
		Env: []types.MCPEnv{{MCPHeader: types.MCPHeader{
			Key: key, Required: true,
		}}},
		RemoteConfig: &types.RemoteRuntimeConfig{
			IsTemplate:  true,
			URLTemplate: "https://example.com/mcp/${WORKSPACE}",
			Headers: []types.MCPHeader{{
				Key: key, SecretBinding: &types.MCPSecretBinding{Name: "remote-secret", Key: "token"},
			}},
		},
	}
	input := map[string]string{key: "user-value"}
	options := mcp.ValidationOptions{RemoteMCPURLValidationConfig: mcp.RemoteMCPURLValidationConfig{
		AllowLocalhostMCP: true,
		AllowPrivateIPMCP: true,
		AllowLinkLocalMCP: true,
	}}

	merged, err := prepareTempServerConfig(t.Context(), localK8sClient, namespace, label, &manifest, input, false, options)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp/user-value", manifest.RemoteConfig.URL)
	require.NotContains(t, manifest.RemoteConfig.URL, "secret-value")
	require.Equal(t, "secret-value", merged[key])
	require.Equal(t, "user-value", input[key])
}

func TestPrepareCatalogManifestStoresReferencesAndResolvesCurrentSource(t *testing.T) {
	source := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "component-entry", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Name:           "Component Server",
				Runtime:        types.RuntimeNPX,
				ServerUserType: types.ServerUserTypeSingleUser,
				NPXConfig:      &types.NPXRuntimeConfig{Package: "@example/component"},
			},
		},
	}
	manifest := types.MCPServerCatalogEntryManifest{
		Name:           "Composite",
		Runtime:        types.RuntimeComposite,
		ServerUserType: types.ServerUserTypeSingleUser,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{
				CatalogEntryID: source.Name,
				Manifest:       types.MCPServerCatalogEntryManifest{Name: "Stale", Runtime: types.RuntimeRemote},
				ToolPrefix:     "component_",
			},
		}},
	}
	req := newCompositeCatalogRequest(source)
	handler := &MCPCatalogHandler{mcpBackend: mcp.RuntimeBackendKubernetes, sessionManager: &mcp.SessionManager{}}

	stored, resolved, err := handler.prepareCatalogManifest(req, manifest, system.DefaultCatalog, "")

	require.NoError(t, err)
	require.Equal(t, types.MCPServerCatalogEntryManifest{}, stored.CompositeConfig.ComponentServers[0].Manifest)
	require.Equal(t, "Component Server", resolved.CompositeConfig.ComponentServers[0].Manifest.Name)
	require.Equal(t, "component_", stored.CompositeConfig.ComponentServers[0].ToolPrefix)
	require.Equal(t, "Stale", manifest.CompositeConfig.ComponentServers[0].Manifest.Name)
}

func TestPrepareCatalogManifestRejectsWorkspaceComposite(t *testing.T) {
	manifest := types.MCPServerCatalogEntryManifest{
		Name:           "Composite",
		Runtime:        types.RuntimeComposite,
		ServerUserType: types.ServerUserTypeSingleUser,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{CatalogEntryID: "component-entry"},
		}},
	}
	handler := &MCPCatalogHandler{mcpBackend: mcp.RuntimeBackendKubernetes, sessionManager: &mcp.SessionManager{}}

	_, _, err := handler.prepareCatalogManifest(newCompositeCatalogRequest(), manifest, "", "workspace-1")

	require.ErrorContains(t, err, "only supported in the default catalog")
}

func TestResolveCompositeCatalogEntryReturnsLiveManifestAndSourceStatus(t *testing.T) {
	source := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "component-entry", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Name:           "Live source",
				Runtime:        types.RuntimeRemote,
				ServerUserType: types.ServerUserTypeSingleUser,
				RemoteConfig:   &types.RemoteCatalogConfig{FixedURL: "https://live.example/mcp", StaticOAuthRequired: true},
			},
		},
	}
	composite := v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "composite", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeComposite,
				CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
					{CatalogEntryID: source.Name},
				}},
			},
		},
	}
	req := newCompositeCatalogRequest(source)

	response, resolved, err := resolveCompositeCatalogEntry(t.Context(), req.Storage, composite)

	require.NoError(t, err)
	require.Equal(t, "https://live.example/mcp", response.Spec.Manifest.CompositeConfig.ComponentServers[0].Manifest.RemoteConfig.FixedURL)
	require.True(t, resolvedSourcesRequireStaticOAuth(resolved))
	require.Equal(t, types.MCPServerCatalogEntryManifest{}, composite.Spec.Manifest.CompositeConfig.ComponentServers[0].Manifest)
}

func newCompositeCatalogRequest(objects ...client.Object) api.Context {
	return api.Context{
		Request:        httptest.NewRequest(http.MethodGet, "/", nil),
		ResponseWriter: httptest.NewRecorder(),
		Storage: storage.Client(fake.NewClientBuilder().
			WithScheme(storagescheme.Scheme).
			WithObjects(objects...).
			Build()),
		User: testUserWithRole("admin", types.GroupAdmin),
	}
}
