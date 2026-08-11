package mcp

import (
	"net/http"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// adminConfigComposite builds a composite catalog entry manifest from the given components.
func adminConfigComposite(components ...types.CatalogComponentServer) types.MCPServerCatalogEntryManifest {
	return types.MCPServerCatalogEntryManifest{
		Runtime:         types.RuntimeComposite,
		CompositeConfig: &types.CompositeCatalogConfig{ComponentServers: components},
	}
}

// adminConfigComponentEntry builds the catalog entry a composite component references.
func adminConfigComponentEntry(name string, manifest types.MCPServerCatalogEntryManifest) *v1.MCPServerCatalogEntry {
	return &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1.MCPServerCatalogEntrySpec{Manifest: manifest},
	}
}

func TestEntryMissingAdminConfig(t *testing.T) {
	const ns = "obot-ns"

	newClient := func(t *testing.T, objects ...kclient.Object) kclient.Client {
		t.Helper()
		scheme := runtime.NewScheme()
		require.NoError(t, corev1.AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}
	secret := func(name string, data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{Data: data, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"label": ""}}}
	}

	tests := []struct {
		name            string
		manifest        types.MCPServerCatalogEntryManifest
		components      []*v1.MCPServerCatalogEntry
		oauthConfigured bool
		client          kclient.Client
		wantFields      []string
		wantOAuth       bool
	}{
		{
			name: "required env resolved binding",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeNPX,
				Env: []types.MCPEnv{{
					MCPHeader: types.MCPHeader{
						Key:           "TOKEN",
						Required:      true,
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					},
				}},
			},
			client: newClient(t, secret("s", map[string][]byte{"k": []byte("v")})),
		},
		{
			name: "required env missing binding",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeNPX,
				Env: []types.MCPEnv{{
					MCPHeader: types.MCPHeader{
						Key:           "TOKEN",
						Required:      true,
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					},
				}},
			},
			client:     newClient(t),
			wantFields: []string{"env TOKEN"},
		},
		{
			name: "non-required env missing binding",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeNPX,
				Env: []types.MCPEnv{{
					MCPHeader: types.MCPHeader{
						Key:           "TOKEN",
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					},
				}},
			},
			client:     newClient(t),
			wantFields: []string{"env TOKEN"},
		},
		{
			name: "required env empty binding",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeNPX,
				Env: []types.MCPEnv{{
					MCPHeader: types.MCPHeader{
						Key:           "TOKEN",
						Required:      true,
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					},
				}},
			},
			client:     newClient(t, secret("s", map[string][]byte{"k": []byte("")})),
			wantFields: []string{"env TOKEN"},
		},
		{
			name: "required header missing binding",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeRemote,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL: "https://example.com",
					Headers: []types.MCPHeader{{
						Key:           "X-Api-Key",
						Required:      true,
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					}},
				},
			},
			client:     newClient(t),
			wantFields: []string{"header X-Api-Key"},
		},
		{
			name: "static oauth missing",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeRemote,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL:            "https://example.com",
					StaticOAuthRequired: true,
				},
			},
			wantOAuth: true,
		},
		{
			name: "static oauth configured",
			manifest: types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeRemote,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL:            "https://example.com",
					StaticOAuthRequired: true,
				},
			},
			oauthConfigured: true,
		},
		{
			// A composite binds nothing itself; what it can be missing is read from the entry
			// each component references.
			name:     "composite component missing binding",
			manifest: adminConfigComposite(types.CatalogComponentServer{CatalogEntryID: "c1"}),
			components: []*v1.MCPServerCatalogEntry{adminConfigComponentEntry("c1", types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeNPX,
				Env: []types.MCPEnv{{
					MCPHeader: types.MCPHeader{
						Key:           "TOKEN",
						Required:      true,
						SecretBinding: &types.MCPSecretBinding{Name: "s", Key: "k"},
					},
				}},
			})},
			client:     newClient(t),
			wantFields: []string{"component c1 env TOKEN"},
		},
		{
			name:       "composite component source no longer exists",
			manifest:   adminConfigComposite(types.CatalogComponentServer{CatalogEntryID: "gone"}),
			client:     newClient(t),
			wantFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := v1.MCPServerCatalogEntry{
				Spec:   v1.MCPServerCatalogEntrySpec{Manifest: tt.manifest},
				Status: v1.MCPServerCatalogEntryStatus{OAuthCredentialConfigured: tt.oauthConfigured},
			}

			storage := fake.NewClientBuilder().WithScheme(storagescheme.Scheme)
			for _, component := range tt.components {
				storage = storage.WithObjects(component)
			}

			got, err := EntryMissingAdminConfig(t.Context(), storage.Build(), tt.client, ns, entry, "label")
			require.NoError(t, err)
			assert.Equal(t, tt.wantFields, got.SecretBoundFields)
			assert.Equal(t, tt.wantOAuth, got.StaticOAuth)

			err = got.Err("entry")
			if len(tt.wantFields) == 0 && !tt.wantOAuth {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			errHTTP, ok := err.(*types.ErrHTTP)
			require.True(t, ok)
			assert.Equal(t, http.StatusBadRequest, errHTTP.Code)
			assert.Contains(t, errHTTP.Message, "catalog entry entry cannot be connected")
		})
	}
}
