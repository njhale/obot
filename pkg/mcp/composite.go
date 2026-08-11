package mcp

import (
	"context"
	"fmt"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolvedComponent pairs a composite component reference with the live source it points at.
// Exactly one of Entry or Server is set when the reference resolves; both are nil when it does not.
type ResolvedComponent struct {
	Ref    types.CatalogComponentServer
	Entry  *v1.MCPServerCatalogEntry
	Server *v1.MCPServer
}

// Unresolved reports whether the reference did not point at a live source.
func (r ResolvedComponent) Unresolved() bool {
	return r.Entry == nil && r.Server == nil
}

// CatalogEntryManifest returns the component's source configuration in catalog entry form.
// Multi-user server components are converted so that both kinds of component present the
// same shape. The zero manifest is returned for an unresolved reference.
func (r ResolvedComponent) CatalogEntryManifest() types.MCPServerCatalogEntryManifest {
	switch {
	case r.Entry != nil:
		return r.Entry.Spec.Manifest
	case r.Server != nil:
		return r.Server.Spec.Manifest.ConvertToCatalogEntry()
	}
	return types.MCPServerCatalogEntryManifest{}
}

// ResolveComponents looks up the live source behind every component reference, preserving the
// order of components. A reference that does not resolve yields an unresolved ResolvedComponent
// rather than an error, so one deleted source cannot fail a whole composite.
func ResolveComponents(ctx context.Context, c kclient.Client, namespace string, components []types.CatalogComponentServer) ([]ResolvedComponent, error) {
	resolved := make([]ResolvedComponent, 0, len(components))
	for _, component := range components {
		result := ResolvedComponent{Ref: component}

		switch {
		case component.MCPServerID != "":
			var server v1.MCPServer
			err := c.Get(ctx, kclient.ObjectKey{Namespace: namespace, Name: component.MCPServerID}, &server)
			if err == nil {
				result.Server = &server
			} else if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to get multi-user server %q: %w", component.MCPServerID, err)
			}
		case component.CatalogEntryID != "":
			var entry v1.MCPServerCatalogEntry
			err := c.Get(ctx, kclient.ObjectKey{Namespace: namespace, Name: component.CatalogEntryID}, &entry)
			if err == nil {
				result.Entry = &entry
			} else if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to get component catalog entry %q: %w", component.CatalogEntryID, err)
			}
		}

		resolved = append(resolved, result)
	}

	return resolved, nil
}

// ValidateComponentRefs resolves and validates every component reference of a composite catalog
// entry. It enforces the rules that require the referenced object, which the manifest validators
// cannot see on their own.
func ValidateComponentRefs(ctx context.Context, c kclient.Client, namespace, catalogName, workspaceID string, components []types.CatalogComponentServer) error {
	// Composite entries are catalog-scoped for now. This is the single choke point for composite
	// writes, so the restriction lives here rather than on any one endpoint.
	if workspaceID != "" {
		return fmt.Errorf("composite catalog entries are not supported in power user workspaces")
	}

	resolved, err := ResolveComponents(ctx, c, namespace, components)
	if err != nil {
		return err
	}

	for _, component := range resolved {
		if err := ValidateComponent(component, catalogName, workspaceID); err != nil {
			return err
		}
	}

	return nil
}

// ValidateComponent validates a single resolved component against the composite's scope.
// An unresolved reference is not an error here: callers decide whether a missing source should
// be dropped, skipped, or surfaced.
func ValidateComponent(component ResolvedComponent, catalogName, workspaceID string) error {
	hasCatalogEntry, hasServer := component.Ref.CatalogEntryID != "", component.Ref.MCPServerID != ""
	switch {
	case hasCatalogEntry && hasServer:
		return fmt.Errorf("component cannot have both catalogEntryID and mcpServerID set")
	case !hasCatalogEntry && !hasServer:
		return fmt.Errorf("component must have either catalogEntryID or mcpServerID set")
	}

	if server := component.Server; server != nil {
		if server.Spec.IsSingleUser() {
			return fmt.Errorf("server %q is not a multi-user server", component.Ref.MCPServerID)
		}
		if catalogName != "" && server.Spec.MCPCatalogID != catalogName {
			return fmt.Errorf("multi-user server %q not found in catalog %q", component.Ref.MCPServerID, catalogName)
		}
		if server.Spec.Manifest.Runtime == types.RuntimeComposite {
			return fmt.Errorf("multi-user server %q cannot be included in a composite server because it is itself composite", component.Ref.MCPServerID)
		}
		return nil
	}

	entry := component.Entry
	if entry == nil {
		return nil
	}

	if catalogName != "" && entry.Spec.MCPCatalogName != catalogName {
		return fmt.Errorf("component catalog entry %q not found in catalog %q", component.Ref.CatalogEntryID, catalogName)
	}
	if workspaceID != "" && entry.Spec.PowerUserWorkspaceID != workspaceID {
		return fmt.Errorf("component catalog entry %q not found in workspace %q", component.Ref.CatalogEntryID, workspaceID)
	}
	if entry.Spec.Manifest.Runtime == types.RuntimeComposite {
		return fmt.Errorf("catalog entry %q cannot be included in a composite server because it is itself composite", component.Ref.CatalogEntryID)
	}
	if entry.Spec.Manifest.ServerUserType == types.ServerUserTypeMultiUser {
		return fmt.Errorf("multi-user catalog entry %q cannot be included in a composite server; use the multi-user MCP server instead", component.Ref.CatalogEntryID)
	}
	return nil
}

// EntryRequiresStaticOAuthCreds reports whether an entry cannot be used yet because an
// administrator has not configured the static OAuth credentials it needs. A composite declares
// none of its own: it is blocked while any component it exposes is, because the credential is
// keyed on the component's catalog entry, which is exactly what the composite references.
func EntryRequiresStaticOAuthCreds(ctx context.Context, c kclient.Client, entry v1.MCPServerCatalogEntry) (bool, error) {
	if entry.Spec.Manifest.Runtime == types.RuntimeComposite {
		if entry.Spec.Manifest.CompositeConfig == nil {
			return false, nil
		}

		resolved, err := ResolveComponents(ctx, c, entry.Namespace, entry.Spec.Manifest.CompositeConfig.ComponentServers)
		if err != nil {
			return false, err
		}

		for _, component := range resolved {
			// A multi-user component proxies to a server an administrator already runs, and an
			// unresolved one never materializes, so neither can be waiting on credentials here.
			if component.Entry != nil && componentEntryRequiresStaticOAuthCreds(*component.Entry) {
				return true, nil
			}
		}
		return false, nil
	}

	return componentEntryRequiresStaticOAuthCreds(entry), nil
}

// componentEntryRequiresStaticOAuthCreds answers EntryRequiresStaticOAuthCreds for an entry that
// declares its own remote configuration, using the status the controller keeps up to date rather
// than looking the credential up per call.
func componentEntryRequiresStaticOAuthCreds(entry v1.MCPServerCatalogEntry) bool {
	if entry.Spec.Manifest.RemoteConfig == nil || !entry.Spec.Manifest.RemoteConfig.StaticOAuthRequired {
		return false
	}
	return !entry.Status.OAuthCredentialConfigured
}
