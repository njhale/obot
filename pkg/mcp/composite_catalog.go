package mcp

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolvedCatalogManifest contains a live view of a catalog manifest and the
// source resources used to build it. Source resources are included because
// some callers need status that is intentionally not copied into a manifest.
type ResolvedCatalogManifest struct {
	Manifest       types.MCPServerCatalogEntryManifest
	CatalogEntries map[string]v1.MCPServerCatalogEntry
	MCPServers     map[string]v1.MCPServer
}

// MissingStaticOAuthCredentials reports whether any resolved catalog-entry
// source requires static OAuth but has not been configured. Credential values
// remain owned by the source entry and are never loaded by the resolver.
func (r ResolvedCatalogManifest) MissingStaticOAuthCredentials() bool {
	for _, entry := range r.CatalogEntries {
		if entry.Spec.Manifest.RemoteConfig != nil &&
			entry.Spec.Manifest.RemoteConfig.StaticOAuthRequired &&
			!entry.Status.OAuthCredentialConfigured {
			return true
		}
	}
	return false
}

// CompositeCatalogResolver resolves refs-only composite catalog manifests.
// Composites are supported only in the default catalog.
type CompositeCatalogResolver struct {
	reader kclient.Reader

	// catalogEntries contains objects in the current desired state that may not
	// have been persisted yet, such as entries in one Git catalog sync.
	catalogEntries map[string]v1.MCPServerCatalogEntry
}

func NewCompositeCatalogResolver(reader kclient.Reader) *CompositeCatalogResolver {
	return &CompositeCatalogResolver{reader: reader}
}

// WithCatalogEntries returns a resolver that checks entries before persisted
// storage. This is used by Git catalog reconciliation.
func (r *CompositeCatalogResolver) WithCatalogEntries(entries map[string]v1.MCPServerCatalogEntry) *CompositeCatalogResolver {
	clone := *r
	clone.catalogEntries = entries
	return &clone
}

// Resolve returns a live copy of manifest with every catalog component
// manifest populated from its source. It never mutates manifest.
func (r *CompositeCatalogResolver) Resolve(ctx context.Context, manifest types.MCPServerCatalogEntryManifest) (types.MCPServerCatalogEntryManifest, error) {
	resolved, err := r.ResolveDetailed(ctx, manifest)
	return resolved.Manifest, err
}

// ResolveDetailed is Resolve plus the source resources used for hydration.
func (r *CompositeCatalogResolver) ResolveDetailed(ctx context.Context, manifest types.MCPServerCatalogEntryManifest) (ResolvedCatalogManifest, error) {
	result := ResolvedCatalogManifest{
		Manifest:       *manifest.DeepCopy(),
		CatalogEntries: map[string]v1.MCPServerCatalogEntry{},
		MCPServers:     map[string]v1.MCPServer{},
	}
	if result.Manifest.Runtime != types.RuntimeComposite {
		return result, nil
	}
	if result.Manifest.CompositeConfig == nil {
		return ResolvedCatalogManifest{}, fmt.Errorf("composite configuration is required")
	}

	for i := range result.Manifest.CompositeConfig.ComponentServers {
		component := &result.Manifest.CompositeConfig.ComponentServers[i]
		if err := r.resolveComponent(ctx, component, &result); err != nil {
			return ResolvedCatalogManifest{}, fmt.Errorf("resolve component %d: %w", i, err)
		}
	}

	return result, nil
}

func (r *CompositeCatalogResolver) resolveComponent(ctx context.Context, component *types.CatalogComponentServer, result *ResolvedCatalogManifest) error {
	hasCatalogEntry := component.CatalogEntryID != ""
	hasMCPServer := component.MCPServerID != ""
	if hasCatalogEntry == hasMCPServer {
		return fmt.Errorf("component must contain exactly one source reference")
	}

	if hasCatalogEntry {
		entry, err := r.getCatalogEntry(ctx, component.CatalogEntryID)
		if err != nil {
			return err
		}
		if entry.Spec.MCPCatalogName != system.DefaultCatalog || entry.Spec.PowerUserWorkspaceID != "" {
			return fmt.Errorf("catalog entry %q does not belong to the default catalog", component.CatalogEntryID)
		}
		if entry.Spec.Manifest.ServerUserType == types.ServerUserTypeMultiUser {
			return fmt.Errorf("multi-user catalog entry %q cannot be included in a composite; use its MCP server instead", component.CatalogEntryID)
		}
		if entry.Spec.Manifest.Runtime == types.RuntimeComposite {
			return fmt.Errorf("nested composite catalog entry %q is not supported", component.CatalogEntryID)
		}

		component.Manifest = *entry.Spec.Manifest.DeepCopy()
		result.CatalogEntries[entry.Name] = entry
		return nil
	}

	if r.reader == nil {
		return fmt.Errorf("get MCP server %q: no catalog reader configured", component.MCPServerID)
	}
	var server v1.MCPServer
	if err := r.reader.Get(ctx, kclient.ObjectKey{
		Namespace: system.DefaultNamespace,
		Name:      component.MCPServerID,
	}, &server); err != nil {
		return fmt.Errorf("get MCP server %q: %w", component.MCPServerID, err)
	}
	if server.Spec.IsSingleUser() || server.Spec.MCPCatalogID != system.DefaultCatalog || server.Spec.PowerUserWorkspaceID != "" {
		return fmt.Errorf("MCP server %q is not a default-catalog multi-user server", component.MCPServerID)
	}
	if server.Spec.Manifest.Runtime == types.RuntimeComposite {
		return fmt.Errorf("nested composite MCP server %q is not supported", component.MCPServerID)
	}

	component.Manifest = server.Spec.Manifest.ConvertToCatalogEntry()
	result.MCPServers[server.Name] = server
	return nil
}

func (r *CompositeCatalogResolver) getCatalogEntry(ctx context.Context, name string) (v1.MCPServerCatalogEntry, error) {
	if entry, ok := r.catalogEntries[name]; ok {
		return *entry.DeepCopy(), nil
	}
	if r.reader == nil {
		return v1.MCPServerCatalogEntry{}, fmt.Errorf("get catalog entry %q: no catalog reader configured", name)
	}

	var entry v1.MCPServerCatalogEntry
	if err := r.reader.Get(ctx, kclient.ObjectKey{
		Namespace: system.DefaultNamespace,
		Name:      name,
	}, &entry); err != nil {
		return v1.MCPServerCatalogEntry{}, fmt.Errorf("get catalog entry %q: %w", name, err)
	}
	return entry, nil
}

// StripCatalogComponentManifests removes response-only resolved data before a
// catalog composite is persisted. It returns whether manifest changed.
func StripCatalogComponentManifests(manifest *types.MCPServerCatalogEntryManifest) bool {
	if manifest == nil || manifest.CompositeConfig == nil {
		return false
	}

	changed := false
	empty := types.MCPServerCatalogEntryManifest{}
	for i := range manifest.CompositeConfig.ComponentServers {
		component := &manifest.CompositeConfig.ComponentServers[i]
		if !reflect.DeepEqual(component.Manifest, empty) {
			component.Manifest = empty
			changed = true
		}
	}
	return changed
}

// CompositeCatalogReference describes a stored catalog composite that refers
// to a source catalog entry.
type CompositeCatalogReference struct {
	Entry         v1.MCPServerCatalogEntry
	WillBeDeleted bool
}

// ListCompositeCatalogReferences finds default-catalog composites that refer
// to sourceName. Catalog composites persist references, so no resolution is
// needed for this integrity check.
func ListCompositeCatalogReferences(ctx context.Context, reader kclient.Reader, sourceName string) ([]CompositeCatalogReference, error) {
	var entries v1.MCPServerCatalogEntryList
	if err := reader.List(ctx, &entries, kclient.InNamespace(system.DefaultNamespace)); err != nil {
		return nil, fmt.Errorf("list composite catalog entries: %w", err)
	}

	var references []CompositeCatalogReference
	for _, entry := range entries.Items {
		if entry.Spec.MCPCatalogName != system.DefaultCatalog ||
			entry.Spec.PowerUserWorkspaceID != "" ||
			entry.Spec.Manifest.Runtime != types.RuntimeComposite ||
			entry.Spec.Manifest.CompositeConfig == nil {
			continue
		}

		matched := false
		for _, component := range entry.Spec.Manifest.CompositeConfig.ComponentServers {
			if component.CatalogEntryID == sourceName {
				matched = true
				break
			}
		}
		if matched {
			references = append(references, CompositeCatalogReference{
				Entry:         entry,
				WillBeDeleted: len(entry.Spec.Manifest.CompositeConfig.ComponentServers) == 1,
			})
		}
	}
	return references, nil
}

// RemoveCatalogEntryFromComposites removes sourceName from every referencing
// catalog composite. A composite that would become empty is deleted. The
// operation is idempotent and retries concurrent updates.
func RemoveCatalogEntryFromComposites(ctx context.Context, client kclient.Client, sourceName string) error {
	references, err := ListCompositeCatalogReferences(ctx, client, sourceName)
	if err != nil {
		return err
	}

	for _, reference := range references {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var entry v1.MCPServerCatalogEntry
			if err := client.Get(ctx, kclient.ObjectKey{
				Namespace: system.DefaultNamespace,
				Name:      reference.Entry.Name,
			}, &entry); apierrors.IsNotFound(err) {
				return nil
			} else if err != nil {
				return err
			}

			config := entry.Spec.Manifest.CompositeConfig
			if config == nil {
				return nil
			}
			before := len(config.ComponentServers)
			config.ComponentServers = slices.DeleteFunc(config.ComponentServers, func(component types.CatalogComponentServer) bool {
				return component.CatalogEntryID == sourceName
			})
			if len(config.ComponentServers) == before {
				return nil
			}

			entry.Spec.Manifest.ToolPreview = nil
			StripCatalogComponentManifests(&entry.Spec.Manifest)
			if len(config.ComponentServers) == 0 {
				return kclient.IgnoreNotFound(client.Delete(ctx, &entry))
			}
			return client.Update(ctx, &entry)
		}); err != nil {
			return fmt.Errorf("remove catalog entry %q from composite %q: %w", sourceName, reference.Entry.Name, err)
		}
	}

	return nil
}
