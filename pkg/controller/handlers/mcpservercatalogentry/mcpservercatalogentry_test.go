package mcpservercatalogentry

import (
	"testing"

	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMigrateCompositeCatalogStorageStripsSnapshotsAndPrunesNestedComponents(t *testing.T) {
	entry := newMCPServerCatalogEntry("composite-entry", types.MCPServerCatalogEntryManifest{
		Name:    "Composite Entry",
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{
				CatalogEntryID: "component-entry",
				Manifest: types.MCPServerCatalogEntryManifest{
					Name: "Component snapshot", Runtime: types.RuntimeRemote,
				},
			},
			{
				CatalogEntryID: "legacy-nested-entry",
				Manifest: types.MCPServerCatalogEntryManifest{
					Name: "Nested snapshot", Runtime: types.RuntimeComposite,
				},
			},
		}},
	})
	client := newFakeClient(entry)

	req := router.Request{
		Client: client, Ctx: t.Context(), Object: entry,
		Namespace: entry.Namespace, Name: entry.Name,
	}
	require.NoError(t, (&Handler{}).MigrateCompositeCatalogStorage(req, &router.ResponseWrapper{}))

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	require.Len(t, updated.Spec.Manifest.CompositeConfig.ComponentServers, 1)
	assert.Equal(t, "component-entry", updated.Spec.Manifest.CompositeConfig.ComponentServers[0].CatalogEntryID)
	assert.Empty(t, updated.Spec.Manifest.CompositeConfig.ComponentServers[0].Manifest)

	// Reconciliation is idempotent once the refs-only form is stored.
	req.Object = &updated
	require.NoError(t, (&Handler{}).MigrateCompositeCatalogStorage(req, &router.ResponseWrapper{}))
}

func TestClearCompositeCatalogNeedsUpdate(t *testing.T) {
	entry := newMCPServerCatalogEntry("composite-entry", types.MCPServerCatalogEntryManifest{
		Name: "Composite Entry", Runtime: types.RuntimeComposite,
	})
	entry.Status.NeedsUpdate = true
	client := newFakeClient(entry)

	require.NoError(t, (&Handler{}).ClearCompositeCatalogNeedsUpdate(router.Request{
		Client: client, Ctx: t.Context(), Object: entry,
		Namespace: entry.Namespace, Name: entry.Name,
	}, &router.ResponseWrapper{}))

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), router.Key(entry.Namespace, entry.Name), &updated))
	assert.False(t, updated.Status.NeedsUpdate)
}

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
