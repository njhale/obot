package v1

import (
	"slices"

	"github.com/obot-platform/nah/pkg/fields"
	"github.com/obot-platform/obot/apiclient/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	_ DeleteRefs    = (*MCPServerCatalogEntry)(nil)
	_ fields.Fields = (*MCPServerCatalogEntry)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type MCPServerCatalogEntry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   MCPServerCatalogEntrySpec   `json:"spec"`
	Status MCPServerCatalogEntryStatus `json:"status"`
}

func (in *MCPServerCatalogEntry) GetColumns() [][]string {
	return [][]string{
		{"Name", "Name"},
		{"MCP Catalog", "Spec.MCPCatalogName"},
		{"Created", "{{ago .CreationTimestamp}}"},
	}
}

func (in *MCPServerCatalogEntry) Has(field string) bool {
	return slices.Contains(in.FieldNames(), field)
}

func (in *MCPServerCatalogEntry) Get(field string) string {
	switch field {
	case "spec.mcpCatalogName":
		return in.Spec.MCPCatalogName
	case "spec.powerUserWorkspaceID":
		return in.Spec.PowerUserWorkspaceID
	case "spec.manifest.runtime":
		return string(in.Spec.Manifest.Runtime)
	case "spec.manifest.remoteConfig.tunnelName":
		if in.Spec.Manifest.RemoteConfig != nil {
			return in.Spec.Manifest.RemoteConfig.TunnelName
		}
		return ""
	}
	return ""
}

func (in *MCPServerCatalogEntry) FieldNames() []string {
	return []string{
		"spec.mcpCatalogName",
		"spec.powerUserWorkspaceID",
		"spec.manifest.runtime",
		"spec.manifest.remoteConfig.tunnelName",
	}
}

func (in *MCPServerCatalogEntry) DeleteRefs() []Ref {
	return []Ref{
		{ObjType: &MCPCatalog{}, Name: in.Spec.MCPCatalogName},
		{ObjType: &PowerUserWorkspace{}, Name: in.Spec.PowerUserWorkspaceID},
	}
}

// IsGitManaged mirrors the existing GitOps heuristic: sourced, non-editable,
// non-detached catalog entries are treated as git-managed.
func (in *MCPServerCatalogEntry) IsGitManaged() bool {
	return !in.Spec.Detached && !in.Spec.Editable && in.Spec.SourceURL != ""
}

type MCPServerCatalogEntrySpec struct {
	Manifest         types.MCPServerCatalogEntryManifest `json:"manifest"`
	UnsupportedTools []string                            `json:"unsupportedTools,omitempty"`
	MCPCatalogName   string                              `json:"mcpCatalogName,omitempty"`
	Editable         bool                                `json:"editable,omitempty"`
	Detached         bool                                `json:"detached"`
	SourceURL        string                              `json:"sourceURL,omitempty"`
	// PowerUserWorkspaceID contains the name of the PowerUserWorkspace that owns this catalog entry, if there is one.
	PowerUserWorkspaceID string `json:"powerUserWorkspaceID,omitempty"`
}

type MCPServerCatalogEntryStatus struct {
	// UserCount contains the current number of users with an MCP server created from this catalog entry.
	// For multi-user entries, this is the sum of MCPServerInstanceUserCount across each MCPServer created from this entry (not de-duplicated across servers).
	UserCount int `json:"userCount,omitempty"`
	// LastUpdated is the timestamp when this catalog entry was last updated.
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
	// ToolPreviewsLastGenerated is the timestamp when the tool previews were last generated for this catalog entry.
	ToolPreviewsLastGenerated *metav1.Time `json:"toolPreviewsLastGenerated,omitempty"`
	// ManifestHash is a SHA256 hash of the catalog entry configuration used to detect changes.
	ManifestHash string `json:"manifestHash,omitempty"`
	// ObservedComponents records, keyed by component ID, what was seen for each component of a
	// composite entry the last time this entry's configuration for that component changed.
	// Only components with tool overrides are recorded.
	ObservedComponents map[string]ObservedComponent `json:"observedComponents,omitempty"`
	// OAuthCredentialConfigured indicates whether OAuth credentials have been configured for this remote catalog entry.
	// Only relevant when Runtime is "remote" and RemoteConfig.StaticOAuthRequired is true.
	OAuthCredentialConfigured bool `json:"oauthCredentialConfigured,omitempty"`
}

// ObservedComponent records what was seen for one component of a composite catalog entry the
// last time the composite's tool overrides for that component were set. Comparing SourceToolsHash
// against the component source's current tool list reports whether those overrides may have gone
// stale, since an override names a tool the source can rename or drop.
type ObservedComponent struct {
	// ToolOverridesHash is a hash of the composite's tool overrides for the component. A change
	// to it means the overrides were re-set against the source's tool list as it stood then.
	ToolOverridesHash string `json:"toolOverridesHash,omitempty"`
	// SourceToolsHash is a hash of the component source's tool list at that time.
	SourceToolsHash string `json:"sourceToolsHash,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type MCPServerCatalogEntryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []MCPServerCatalogEntry `json:"items"`
}
