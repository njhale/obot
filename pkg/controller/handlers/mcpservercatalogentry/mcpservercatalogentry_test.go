package mcpservercatalogentry

import (
	"testing"

	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(objects ...kclient.Object) kclient.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(storagescheme.Scheme).
		WithStatusSubresource(&v1.MCPServerCatalogEntry{}).
		WithIndex(&v1.MCPServer{}, "spec.mcpServerCatalogEntryName", func(obj kclient.Object) []string {
			server := obj.(*v1.MCPServer)
			if server.Spec.MCPServerCatalogEntryName == "" {
				return nil
			}
			return []string{server.Spec.MCPServerCatalogEntryName}
		}).
		WithObjects(objects...).
		Build()
}

func newMCPServerCatalogEntry(name string, manifest types.MCPServerCatalogEntryManifest) *v1.MCPServerCatalogEntry {
	return &v1.MCPServerCatalogEntry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "MCPServerCatalogEntry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1.MCPServerCatalogEntrySpec{
			Manifest: manifest,
		},
	}
}

func newCompositeEntry(name string, components ...types.CatalogComponentServer) *v1.MCPServerCatalogEntry {
	return newMCPServerCatalogEntry(name, types.MCPServerCatalogEntryManifest{
		Name:            "Composite",
		Runtime:         types.RuntimeComposite,
		ServerUserType:  types.ServerUserTypeSingleUser,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: components},
	})
}

func newComponentEntry(name string, tools ...string) *v1.MCPServerCatalogEntry {
	toolPreview := make([]types.MCPServerTool, 0, len(tools))
	for _, tool := range tools {
		toolPreview = append(toolPreview, types.MCPServerTool{Name: tool})
	}

	return newMCPServerCatalogEntry(name, types.MCPServerCatalogEntryManifest{
		Name:           "Component",
		Runtime:        types.RuntimeNPX,
		ServerUserType: types.ServerUserTypeSingleUser,
		NPXConfig:      &types.NPXRuntimeConfig{Package: "@example/component"},
		ToolPreview:    toolPreview,
	})
}

func observeComponents(t *testing.T, client kclient.WithWatch, entry *v1.MCPServerCatalogEntry) v1.MCPServerCatalogEntry {
	t.Helper()

	require.NoError(t, (&Handler{}).EnsureObservedComponents(router.Request{
		Client:    client,
		Ctx:       t.Context(),
		Object:    entry,
		Namespace: entry.Namespace,
		Name:      entry.Name,
	}, &router.ResponseWrapper{}))

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	return updated
}

func TestEnsureObservedComponentsRecordsSourceToolsForOverriddenComponents(t *testing.T) {
	component := newComponentEntry("component", "search", "fetch")
	composite := newCompositeEntry("composite", types.CatalogComponentServer{
		CatalogEntryID: component.Name,
		ToolOverrides:  []types.ToolOverride{{Name: "search", Enabled: true}},
	})

	client := newFakeClient(composite, component)
	updated := observeComponents(t, client, composite)

	recorded, ok := updated.Status.ObservedComponents[component.Name]
	require.True(t, ok, "component with tool overrides should be recorded")
	assert.NotEmpty(t, recorded.ToolOverridesHash)
	assert.NotEmpty(t, recorded.SourceToolsHash)
}

func TestEnsureObservedComponentsIgnoresComponentsWithoutOverrides(t *testing.T) {
	component := newComponentEntry("component", "search")
	composite := newCompositeEntry("composite", types.CatalogComponentServer{
		CatalogEntryID: component.Name,
		ToolPrefix:     "c_",
	})

	client := newFakeClient(composite, component)
	updated := observeComponents(t, client, composite)

	assert.Empty(t, updated.Status.ObservedComponents, "nothing can go stale without tool overrides")
}

func TestEnsureObservedComponentsKeepsRecordingWhenSourceToolsChange(t *testing.T) {
	component := newComponentEntry("component", "search", "fetch")
	composite := newCompositeEntry("composite", types.CatalogComponentServer{
		CatalogEntryID: component.Name,
		ToolOverrides:  []types.ToolOverride{{Name: "search", Enabled: true}},
	})

	client := newFakeClient(composite, component)
	original := observeComponents(t, client, composite)

	// The source drops the tool the override names. The recording must not follow it, or the
	// overrides would never read as stale.
	component.Spec.Manifest.ToolPreview = []types.MCPServerTool{{Name: "fetch"}}
	require.NoError(t, client.Update(t.Context(), component))

	updated := observeComponents(t, client, &original)
	assert.Equal(t, original.Status.ObservedComponents, updated.Status.ObservedComponents)
	assert.NotEqual(t,
		utils.Digest(component.Spec.Manifest.ToolPreview),
		updated.Status.ObservedComponents[component.Name].SourceToolsHash,
		"recorded tool list should still describe the source as it was when the overrides were set")
}

func TestEnsureObservedComponentsRebaselinesWhenOverridesAreReset(t *testing.T) {
	component := newComponentEntry("component", "search", "fetch")
	composite := newCompositeEntry("composite", types.CatalogComponentServer{
		CatalogEntryID: component.Name,
		ToolOverrides:  []types.ToolOverride{{Name: "search", Enabled: true}},
	})

	client := newFakeClient(composite, component)
	original := observeComponents(t, client, composite)

	component.Spec.Manifest.ToolPreview = []types.MCPServerTool{{Name: "fetch"}}
	require.NoError(t, client.Update(t.Context(), component))

	// The admin revisits the overrides against the source's current tools.
	reset := original.DeepCopy()
	reset.Spec.Manifest.CompositeConfig.ComponentServers[0].ToolOverrides = []types.ToolOverride{
		{Name: "fetch", Enabled: true},
	}
	require.NoError(t, client.Update(t.Context(), reset))

	updated := observeComponents(t, client, reset)
	assert.Equal(t,
		utils.Digest(component.Spec.Manifest.ToolPreview),
		updated.Status.ObservedComponents[component.Name].SourceToolsHash)
}

func TestEnsureObservedComponentsKeepsRecordingForUnresolvableSource(t *testing.T) {
	component := newComponentEntry("component", "search")
	composite := newCompositeEntry("composite", types.CatalogComponentServer{
		CatalogEntryID: component.Name,
		ToolOverrides:  []types.ToolOverride{{Name: "search", Enabled: true}},
	})

	client := newFakeClient(composite, component)
	original := observeComponents(t, client, composite)

	require.NoError(t, client.Delete(t.Context(), component))

	updated := observeComponents(t, client, &original)
	assert.Equal(t, original.Status.ObservedComponents, updated.Status.ObservedComponents,
		"a source that is gone should not re-baseline the recording")
}

func TestEnsureObservedComponentsClearsRecordingForNonComposite(t *testing.T) {
	entry := newComponentEntry("entry", "search")
	entry.Status.ObservedComponents = map[string]v1.ObservedComponent{
		"stale": {ToolOverridesHash: "a", SourceToolsHash: "b"},
	}

	client := newFakeClient(entry)
	updated := observeComponents(t, client, entry)

	assert.Nil(t, updated.Status.ObservedComponents)
}

func TestEnsureUserCountMultiUserEntry(t *testing.T) {
	entry := newMCPServerCatalogEntry("multi-entry", types.MCPServerCatalogEntryManifest{
		Name:           "Multi User Template",
		Runtime:        types.RuntimeContainerized,
		ServerUserType: types.ServerUserTypeMultiUser,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
			Port:  8080,
			Path:  "/mcp",
		},
	})

	server1 := newMCPServer("server-1", types.MCPServerManifest{
		Runtime: types.RuntimeContainerized,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
		},
	})
	server1.Spec.MCPServerCatalogEntryName = entry.Name
	server1.Spec.UserID = "admin1"
	server1.Status.MCPServerInstanceUserCount = new(2)

	server2 := newMCPServer("server-2", types.MCPServerManifest{
		Runtime: types.RuntimeContainerized,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
		},
	})
	server2.Spec.MCPServerCatalogEntryName = entry.Name
	server2.Spec.UserID = "admin2"
	server2.Status.MCPServerInstanceUserCount = new(1)

	client := newFakeClient(entry, server1, server2)
	err := (&Handler{}).EnsureUserCount(router.Request{
		Client:    client,
		Ctx:       t.Context(),
		Object:    entry,
		Namespace: entry.Namespace,
		Name:      entry.Name,
	}, &router.ResponseWrapper{})
	require.NoError(t, err)

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	assert.Equal(t, 3, updated.Status.UserCount, "should sum server instance user counts across servers")
}

func TestEnsureUserCountMultiUserEntryExcludesComposite(t *testing.T) {
	entry := newMCPServerCatalogEntry("multi-entry", types.MCPServerCatalogEntryManifest{
		Name:           "Multi User Template",
		Runtime:        types.RuntimeContainerized,
		ServerUserType: types.ServerUserTypeMultiUser,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
			Port:  8080,
			Path:  "/mcp",
		},
	})

	activeServer := newMCPServer("active-server", types.MCPServerManifest{
		Runtime: types.RuntimeContainerized,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
		},
	})
	activeServer.Spec.MCPServerCatalogEntryName = entry.Name
	activeServer.Spec.UserID = "admin1"
	activeServer.Status.MCPServerInstanceUserCount = new(1)

	compositeChild := newMCPServer("composite-child", types.MCPServerManifest{
		Runtime: types.RuntimeContainerized,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
		},
	})
	compositeChild.Spec.MCPServerCatalogEntryName = entry.Name
	compositeChild.Spec.UserID = "admin2"
	compositeChild.Spec.CompositeName = "parent-composite"
	compositeChild.Status.MCPServerInstanceUserCount = new(1)

	client := newFakeClient(entry, activeServer, compositeChild)
	err := (&Handler{}).EnsureUserCount(router.Request{
		Client:    client,
		Ctx:       t.Context(),
		Object:    entry,
		Namespace: entry.Namespace,
		Name:      entry.Name,
	}, &router.ResponseWrapper{})
	require.NoError(t, err)

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	assert.Equal(t, 1, updated.Status.UserCount, "should only count active non-composite servers")
}

func TestEnsureUserCountSingleUserEntryCountsUniqueServerUsers(t *testing.T) {
	entry := newMCPServerCatalogEntry("single-entry", types.MCPServerCatalogEntryManifest{
		Name:           "Single User Template",
		Runtime:        types.RuntimeContainerized,
		ServerUserType: types.ServerUserTypeSingleUser,
		ContainerizedConfig: &types.ContainerizedRuntimeConfig{
			Image: "example/mcp:1.0.0",
			Port:  8080,
			Path:  "/mcp",
		},
	})

	server1 := newMCPServer("server-1", types.MCPServerManifest{Runtime: types.RuntimeContainerized})
	server1.Spec.MCPServerCatalogEntryName = entry.Name
	server1.Spec.UserID = "user1"

	server2 := newMCPServer("server-2", types.MCPServerManifest{Runtime: types.RuntimeContainerized})
	server2.Spec.MCPServerCatalogEntryName = entry.Name
	server2.Spec.UserID = "user1"

	server3 := newMCPServer("server-3", types.MCPServerManifest{Runtime: types.RuntimeContainerized})
	server3.Spec.MCPServerCatalogEntryName = entry.Name
	server3.Spec.UserID = "user2"

	client := newFakeClient(entry, server1, server2, server3)
	err := (&Handler{}).EnsureUserCount(router.Request{
		Client:    client,
		Ctx:       t.Context(),
		Object:    entry,
		Namespace: entry.Namespace,
		Name:      entry.Name,
	}, &router.ResponseWrapper{})
	require.NoError(t, err)

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	assert.Equal(t, 2, updated.Status.UserCount, "should only count active non-composite server")
}

func newMCPServer(name string, manifest types.MCPServerManifest) *v1.MCPServer {
	return &v1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1.SchemeGroupVersion.String(),
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1.MCPServerSpec{
			Manifest: manifest,
		},
	}
}
