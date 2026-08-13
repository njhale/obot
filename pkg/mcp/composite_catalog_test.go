package mcp

import (
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCompositeCatalogResolverResolvesCurrentSourcesWithoutMutatingInput(t *testing.T) {
	source := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Name:           "Current source",
				Runtime:        types.RuntimeRemote,
				ServerUserType: types.ServerUserTypeSingleUser,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL:            "https://current.example/mcp",
					StaticOAuthRequired: true,
				},
			},
		},
		Status: v1.MCPServerCatalogEntryStatus{OAuthCredentialConfigured: true},
	}
	server := &v1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerSpec{
			MCPCatalogID: system.DefaultCatalog,
			Manifest: types.MCPServerManifest{
				Name:    "Shared server",
				Runtime: types.RuntimeNPX,
				NPXConfig: &types.NPXRuntimeConfig{
					Package: "shared-package",
				},
			},
		},
	}
	reader := fake.NewClientBuilder().
		WithScheme(storagescheme.Scheme).
		WithObjects(source, server).
		Build()
	manifest := types.MCPServerCatalogEntryManifest{
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{
				CatalogEntryID: "source",
				Manifest: types.MCPServerCatalogEntryManifest{
					Name:    "Stale source",
					Runtime: types.RuntimeRemote,
					RemoteConfig: &types.RemoteCatalogConfig{
						FixedURL: "https://stale.example/mcp",
					},
				},
				ToolPrefix: "source_",
			},
			{MCPServerID: "shared", ToolPrefix: "shared_"},
		}},
	}

	resolved, err := NewCompositeCatalogResolver(reader).ResolveDetailed(t.Context(), manifest)
	require.NoError(t, err)
	require.Equal(t, "https://current.example/mcp", resolved.Manifest.CompositeConfig.ComponentServers[0].Manifest.RemoteConfig.FixedURL)
	require.Equal(t, "shared-package", resolved.Manifest.CompositeConfig.ComponentServers[1].Manifest.NPXConfig.Package)
	require.Equal(t, "source_", resolved.Manifest.CompositeConfig.ComponentServers[0].ToolPrefix)
	require.True(t, resolved.CatalogEntries["source"].Status.OAuthCredentialConfigured)
	require.Equal(t, "Shared server", resolved.MCPServers["shared"].Spec.Manifest.Name)
	require.Equal(t, "https://stale.example/mcp", manifest.CompositeConfig.ComponentServers[0].Manifest.RemoteConfig.FixedURL)
}

func TestCompositeCatalogResolverUsesDesiredEntryOverlay(t *testing.T) {
	persisted := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Name:           "Persisted",
				Runtime:        types.RuntimeRemote,
				ServerUserType: types.ServerUserTypeSingleUser,
			},
		},
	}
	desired := *persisted.DeepCopy()
	desired.Spec.Manifest.Name = "Desired"
	reader := fake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(persisted).Build()
	manifest := types.MCPServerCatalogEntryManifest{
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{CatalogEntryID: "source"},
		}},
	}

	resolved, err := NewCompositeCatalogResolver(reader).
		WithCatalogEntries(map[string]v1.MCPServerCatalogEntry{"source": desired}).
		Resolve(t.Context(), manifest)
	require.NoError(t, err)
	require.Equal(t, "Desired", resolved.CompositeConfig.ComponentServers[0].Manifest.Name)
}

func TestCompositeCatalogResolverRejectsNonDefaultCatalogSources(t *testing.T) {
	workspaceSource := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace-source", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			PowerUserWorkspaceID: "workspace-1",
			Manifest: types.MCPServerCatalogEntryManifest{
				Runtime:        types.RuntimeRemote,
				ServerUserType: types.ServerUserTypeSingleUser,
			},
		},
	}
	reader := fake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(workspaceSource).Build()
	manifest := types.MCPServerCatalogEntryManifest{
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{CatalogEntryID: workspaceSource.Name},
		}},
	}

	_, err := NewCompositeCatalogResolver(reader).Resolve(t.Context(), manifest)
	require.ErrorContains(t, err, "does not belong to the default catalog")
}

func TestStripCatalogComponentManifests(t *testing.T) {
	manifest := types.MCPServerCatalogEntryManifest{
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{
				CatalogEntryID: "source",
				Manifest:       types.MCPServerCatalogEntryManifest{Name: "Snapshot", Runtime: types.RuntimeRemote},
				ToolPrefix:     "source_",
			},
		}},
	}

	require.True(t, StripCatalogComponentManifests(&manifest))
	require.Equal(t, types.MCPServerCatalogEntryManifest{}, manifest.CompositeConfig.ComponentServers[0].Manifest)
	require.Equal(t, "source_", manifest.CompositeConfig.ComponentServers[0].ToolPrefix)
	require.False(t, StripCatalogComponentManifests(&manifest))
}

func TestCompositeValidatorValidatesRefsOnlyStructure(t *testing.T) {
	manifest := types.MCPServerCatalogEntryManifest{
		Runtime: types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
			{CatalogEntryID: "source", ToolPrefix: "source_"},
		}},
	}

	require.NoError(t, (CompositeValidator{}).ValidateCatalogStructure(t.Context(), manifest))

	manifest.CompositeConfig.ComponentServers[0].MCPServerID = "server"
	require.Error(t, (CompositeValidator{}).ValidateCatalogStructure(t.Context(), manifest))
}

func TestRemoveCatalogEntryFromCompositesUpdatesAndDeletesDependents(t *testing.T) {
	retained := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "retained-composite", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Runtime:     types.RuntimeComposite,
				ToolPreview: []types.MCPServerTool{{Name: "stale"}},
				CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
					{CatalogEntryID: "source", Manifest: types.MCPServerCatalogEntryManifest{Name: "snapshot"}},
					{CatalogEntryID: "other", Manifest: types.MCPServerCatalogEntryManifest{Name: "other snapshot"}},
				}},
			},
		},
	}
	deleted := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "deleted-composite", Namespace: system.DefaultNamespace},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: system.DefaultCatalog,
			Manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeComposite,
				CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: []types.CatalogComponentServer{
					{CatalogEntryID: "source"},
				}},
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(retained, deleted).Build()

	references, err := ListCompositeCatalogReferences(t.Context(), client, "source")
	require.NoError(t, err)
	require.Len(t, references, 2)
	require.NoError(t, RemoveCatalogEntryFromComposites(t.Context(), client, "source"))

	var updated v1.MCPServerCatalogEntry
	require.NoError(t, client.Get(t.Context(), kclient.ObjectKey{Namespace: system.DefaultNamespace, Name: retained.Name}, &updated))
	require.Len(t, updated.Spec.Manifest.CompositeConfig.ComponentServers, 1)
	require.Equal(t, "other", updated.Spec.Manifest.CompositeConfig.ComponentServers[0].CatalogEntryID)
	require.Empty(t, updated.Spec.Manifest.CompositeConfig.ComponentServers[0].Manifest)
	require.Empty(t, updated.Spec.Manifest.ToolPreview)

	err = client.Get(t.Context(), kclient.ObjectKey{Namespace: system.DefaultNamespace, Name: deleted.Name}, &updated)
	require.True(t, apierrors.IsNotFound(err))
	require.NoError(t, RemoveCatalogEntryFromComposites(t.Context(), client, "source"))
}
