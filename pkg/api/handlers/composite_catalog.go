package handlers

import (
	"context"
	"fmt"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/mcp"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func resolveCompositeCatalogEntry(ctx context.Context, reader kclient.Reader, entry v1.MCPServerCatalogEntry) (v1.MCPServerCatalogEntry, mcp.ResolvedCatalogManifest, error) {
	response := *entry.DeepCopy()
	if entry.Spec.Manifest.Runtime != types.RuntimeComposite {
		return response, mcp.ResolvedCatalogManifest{Manifest: *entry.Spec.Manifest.DeepCopy()}, nil
	}
	if entry.Spec.MCPCatalogName != system.DefaultCatalog || entry.Spec.PowerUserWorkspaceID != "" {
		return v1.MCPServerCatalogEntry{}, mcp.ResolvedCatalogManifest{}, fmt.Errorf("composite catalog entry %q does not belong to the default catalog", entry.Name)
	}

	resolved, err := mcp.NewCompositeCatalogResolver(reader).ResolveDetailed(ctx, entry.Spec.Manifest)
	if err != nil {
		return v1.MCPServerCatalogEntry{}, mcp.ResolvedCatalogManifest{}, fmt.Errorf("resolve composite catalog entry %q: %w", entry.Name, err)
	}
	response.Spec.Manifest = resolved.Manifest
	return response, resolved, nil
}

func resolvedSourcesRequireStaticOAuth(resolved mcp.ResolvedCatalogManifest) bool {
	return resolved.MissingStaticOAuthCredentials()
}
