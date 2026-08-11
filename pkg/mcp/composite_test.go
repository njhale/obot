package mcp

import (
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func componentEntry(name, catalogName string, manifest types.MCPServerCatalogEntryManifest) *v1.MCPServerCatalogEntry {
	return &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: catalogName,
			Manifest:       manifest,
		},
	}
}

func npxEntryManifest(name string) types.MCPServerCatalogEntryManifest {
	return types.MCPServerCatalogEntryManifest{
		Name:           name,
		Runtime:        types.RuntimeNPX,
		NPXConfig:      &types.NPXRuntimeConfig{Package: "@example/" + name},
		ServerUserType: types.ServerUserTypeSingleUser,
	}
}

func multiUserServer(name, catalogID string) *v1.MCPServer {
	return &v1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerSpec{
			MCPCatalogID: catalogID,
			Manifest: types.MCPServerManifest{
				Name:            name,
				Runtime:         types.RuntimeContainerized,
				MultiUserConfig: &types.MultiUserConfig{},
				ContainerizedConfig: &types.ContainerizedRuntimeConfig{
					Image: "example/" + name,
					Port:  8080,
					Path:  "/mcp",
				},
			},
		},
	}
}

func testClient(objects ...kclient.Object) kclient.Client {
	return fake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(objects...).Build()
}

func TestResolveComponentsPreservesOrderAcrossBothReferenceKinds(t *testing.T) {
	c := testClient(
		componentEntry("gmail", "default", npxEntryManifest("gmail")),
		componentEntry("slack", "default", npxEntryManifest("slack")),
		multiUserServer("shared", "default"),
	)

	resolved, err := ResolveComponents(t.Context(), c, system.DefaultNamespace, []types.CatalogComponentServer{
		{CatalogEntryID: "slack"},
		{MCPServerID: "shared"},
		{CatalogEntryID: "gmail"},
	})

	require.NoError(t, err)
	require.Len(t, resolved, 3)

	require.NotNil(t, resolved[0].Entry)
	assert.Equal(t, "slack", resolved[0].Entry.Name)
	assert.Nil(t, resolved[0].Server)

	require.NotNil(t, resolved[1].Server)
	assert.Equal(t, "shared", resolved[1].Server.Name)
	assert.Nil(t, resolved[1].Entry)

	require.NotNil(t, resolved[2].Entry)
	assert.Equal(t, "gmail", resolved[2].Entry.Name)

	// A multi-user server component presents the same shape as an entry component.
	assert.Equal(t, types.RuntimeContainerized, resolved[1].CatalogEntryManifest().Runtime)
	assert.Equal(t, "@example/gmail", resolved[2].CatalogEntryManifest().NPXConfig.Package)
}

func TestResolveComponentsLeavesDanglingReferencesUnresolvedWithoutError(t *testing.T) {
	c := testClient(componentEntry("gmail", "default", npxEntryManifest("gmail")))

	resolved, err := ResolveComponents(t.Context(), c, system.DefaultNamespace, []types.CatalogComponentServer{
		{CatalogEntryID: "deleted-entry"},
		{CatalogEntryID: "gmail"},
		{MCPServerID: "deleted-server"},
	})

	require.NoError(t, err, "one deleted source must not fail the whole composite")
	require.Len(t, resolved, 3)

	assert.True(t, resolved[0].Unresolved())
	assert.Empty(t, resolved[0].CatalogEntryManifest().Name)
	assert.False(t, resolved[1].Unresolved())
	assert.True(t, resolved[2].Unresolved())
}

func TestValidateComponentRejectsMalformedReferences(t *testing.T) {
	tests := []struct {
		name      string
		component types.CatalogComponentServer
		expected  string
	}{
		{
			name:      "both ids set",
			component: types.CatalogComponentServer{CatalogEntryID: "gmail", MCPServerID: "shared"},
			expected:  "component cannot have both catalogEntryID and mcpServerID set",
		},
		{
			name:      "neither id set",
			component: types.CatalogComponentServer{ToolPrefix: "gmail"},
			expected:  "component must have either catalogEntryID or mcpServerID set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateComponent(ResolvedComponent{Ref: tt.component}, "default", "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidateComponentRejectsIneligibleCatalogEntries(t *testing.T) {
	multiUserManifest := npxEntryManifest("shared")
	multiUserManifest.ServerUserType = types.ServerUserTypeMultiUser
	nestedManifest := types.MCPServerCatalogEntryManifest{
		Name:            "nested",
		Runtime:         types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{},
		ServerUserType:  types.ServerUserTypeSingleUser,
	}

	tests := []struct {
		name        string
		entry       *v1.MCPServerCatalogEntry
		catalogName string
		expected    string
	}{
		{
			name:        "entry in another catalog",
			entry:       componentEntry("gmail", "other-catalog", npxEntryManifest("gmail")),
			catalogName: "default",
			expected:    `component catalog entry "gmail" not found in catalog "default"`,
		},
		{
			name:        "nested composite entry",
			entry:       componentEntry("nested", "default", nestedManifest),
			catalogName: "default",
			expected:    "itself composite",
		},
		{
			name:        "multi-user entry",
			entry:       componentEntry("shared", "default", multiUserManifest),
			catalogName: "default",
			expected:    "use the multi-user MCP server instead",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component := ResolvedComponent{
				Ref:   types.CatalogComponentServer{CatalogEntryID: tt.entry.Name},
				Entry: tt.entry,
			}

			err := ValidateComponent(component, tt.catalogName, "")

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidateComponentRejectsIneligibleServers(t *testing.T) {
	singleUser := multiUserServer("personal", "")
	singleUser.Spec.Manifest.MultiUserConfig = nil

	err := ValidateComponent(ResolvedComponent{
		Ref:    types.CatalogComponentServer{MCPServerID: "personal"},
		Server: singleUser,
	}, "default", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `server "personal" is not a multi-user server`)

	err = ValidateComponent(ResolvedComponent{
		Ref:    types.CatalogComponentServer{MCPServerID: "shared"},
		Server: multiUserServer("shared", "other-catalog"),
	}, "default", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `multi-user server "shared" not found in catalog "default"`)
}

func TestValidateComponentAcceptsUnresolvedReference(t *testing.T) {
	// Callers decide what to do about a missing source; validation must not reject it,
	// otherwise a single deleted entry would block every write to the composite.
	err := ValidateComponent(ResolvedComponent{
		Ref: types.CatalogComponentServer{CatalogEntryID: "deleted-entry"},
	}, "default", "")

	assert.NoError(t, err)
}

func TestValidateComponentRefsRejectsWorkspaceScopedComposites(t *testing.T) {
	c := testClient(componentEntry("gmail", "default", npxEntryManifest("gmail")))

	err := ValidateComponentRefs(t.Context(), c, system.DefaultNamespace, "", "workspace-1", []types.CatalogComponentServer{
		{CatalogEntryID: "gmail"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "composite catalog entries are not supported in power user workspaces")
}

func TestValidateComponentRefsResolvesEveryComponentBeforeAccepting(t *testing.T) {
	nested := componentEntry("nested", "default", types.MCPServerCatalogEntryManifest{
		Name:            "nested",
		Runtime:         types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{},
		ServerUserType:  types.ServerUserTypeSingleUser,
	})
	c := testClient(
		componentEntry("gmail", "default", npxEntryManifest("gmail")),
		multiUserServer("shared", "default"),
		nested,
	)

	require.NoError(t, ValidateComponentRefs(t.Context(), c, system.DefaultNamespace, "default", "", []types.CatalogComponentServer{
		{CatalogEntryID: "gmail"},
		{MCPServerID: "shared"},
	}))

	// A bad component anywhere in the list fails the whole set, not just the first entry.
	err := ValidateComponentRefs(t.Context(), c, system.DefaultNamespace, "default", "", []types.CatalogComponentServer{
		{CatalogEntryID: "gmail"},
		{CatalogEntryID: "nested"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "itself composite")
}

func staticOAuthEntry(name string, configured bool) *v1.MCPServerCatalogEntry {
	entry := componentEntry(name, "default", types.MCPServerCatalogEntryManifest{
		Name:           name,
		Runtime:        types.RuntimeRemote,
		RemoteConfig:   &types.RemoteCatalogConfig{FixedURL: "https://example.com/mcp", StaticOAuthRequired: true},
		ServerUserType: types.ServerUserTypeSingleUser,
	})
	entry.Status.OAuthCredentialConfigured = configured
	return entry
}

func compositeEntry(name string, componentIDs ...string) v1.MCPServerCatalogEntry {
	components := make([]types.CatalogComponentServer, 0, len(componentIDs))
	for _, id := range componentIDs {
		components = append(components, types.CatalogComponentServer{CatalogEntryID: id})
	}

	return *componentEntry(name, "default", types.MCPServerCatalogEntryManifest{
		Name:            name,
		Runtime:         types.RuntimeComposite,
		ServerUserType:  types.ServerUserTypeSingleUser,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: components},
	})
}

func TestValidateComponentAcceptsStaticOAuthCatalogEntry(t *testing.T) {
	// The credential is keyed on the component's own catalog entry, which is exactly what the
	// composite references, so a component needing static OAuth is an ordinary component.
	component := ResolvedComponent{
		Ref:   types.CatalogComponentServer{CatalogEntryID: "remote"},
		Entry: staticOAuthEntry("remote", false),
	}

	require.NoError(t, ValidateComponent(component, "default", ""))
}

func TestEntryRequiresStaticOAuthCredsFollowsComponents(t *testing.T) {
	tests := []struct {
		name     string
		objects  []kclient.Object
		entry    v1.MCPServerCatalogEntry
		expected bool
	}{
		{
			name:     "component is waiting on credentials",
			objects:  []kclient.Object{staticOAuthEntry("remote", false), componentEntry("gmail", "default", npxEntryManifest("gmail"))},
			entry:    compositeEntry("composite", "gmail", "remote"),
			expected: true,
		},
		{
			name:     "component credentials are configured",
			objects:  []kclient.Object{staticOAuthEntry("remote", true)},
			entry:    compositeEntry("composite", "remote"),
			expected: false,
		},
		{
			name:     "no component needs credentials",
			objects:  []kclient.Object{componentEntry("gmail", "default", npxEntryManifest("gmail"))},
			entry:    compositeEntry("composite", "gmail"),
			expected: false,
		},
		{
			// A component that never materializes cannot be waiting on anything, so it must not
			// hide the composite from everyone forever.
			name:     "unresolved component",
			entry:    compositeEntry("composite", "deleted"),
			expected: false,
		},
		{
			name:     "multi-user component",
			objects:  []kclient.Object{multiUserServer("shared", "default")},
			entry:    compositeEntry("composite"),
			expected: false,
		},
		{
			name:     "non-composite entry still answers for itself",
			entry:    *staticOAuthEntry("remote", false),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EntryRequiresStaticOAuthCreds(t.Context(), testClient(tt.objects...), tt.entry)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
