package types

const (
	OAuthAppTypeMicrosoft365 OAuthAppType = "microsoft365"
	OAuthAppTypeSlack        OAuthAppType = "slack"
	OAuthAppTypeNotion       OAuthAppType = "notion"
	OAuthAppTypeHubSpot      OAuthAppType = "hubspot"
	OAuthAppTypeGitHub       OAuthAppType = "github"
	OAuthAppTypeGoogle       OAuthAppType = "google"
	OAuthAppTypeServiceNow   OAuthAppType = "servicenow"
	OAuthAppTypeCustom       OAuthAppType = "custom"
)

type OAuthAppType string

type OAuthApp struct {
	OAuthAppManifest
}

type OAuthAppManifest struct {
	Metadata

	// Type discriminates between OAuth apps and determines any platform specific differences in the OAuth flow executed
	// for the OAuth app.
	// It's required for all OAuth apps.
	Type OAuthAppType `json:"type"`

	// Name is the name of OAuth app.
	Name string `json:"name,omitempty"`

	// ClientID is the client ID used for the OAuth app flow.
	// It's required for all OAuthAppTypes.
	ClientID string `json:"clientID"`

	// ClientSecret is the client secret used for the OAuth flow.
	// It's required for all OAuthAppTypes.
	ClientSecret string `json:"clientSecret,omitempty"`

	// BaseURL is the base URL of the app to integrate with.
	// It's required for ServiceNow OAuth apps.
	BaseURL string `json:"appBaseURL,omitempty"`

	// AuthURL is the URL used to kick off the OAuth flow for the OAuth app.
	// It's required for custom OAuth apps.
	// Well-known defaults are used for all other OAuthAppTypes.
	AuthURL string `json:"authURL,omitempty"`

	// TokenURL is the URL used to request authorization tokens for the OAuth app.
	// It's required for custom OAuth apps.
	// Well-known defaults are used for all other OAuthAppTypes.
	TokenURL string `json:"tokenURL,omitempty"`

	// TenantID is the ID of the Microsoft 365 tenant.
	// It's required for Microsoft 365 OAuth apps.
	TenantID string `json:"tenantID,omitempty"`

	// AppID is the ID of the HubSpot OAuth app.
	// It's required for HubSpot OAuth apps.
	AppID string `json:"appID,omitempty"`

	// OptionalScope is a set of optional scopes used for HubSpot OAuth apps.
	OptionalScope string `json:"optionalScope,omitempty"`

	// Integration correlates the OAuth app to an integration name in the Otto8 OAuth 2.0 cred tool.
	// It's required for all OAuthAppTypes.
	Integration string `json:"integration,omitempty"`

	// Global indicates if the OAuth app is globally applied to all agents.
	// It's optional and defaults to true.
	Global *bool `json:"global,omitempty"`
}

type OAuthAppList List[OAuthApp]

type OAuthAppLoginAuthStatus struct {
	URL           string `json:"url,omitempty"`
	Authenticated bool   `json:"authenticated,omitempty"`
	Required      *bool  `json:"required,omitempty"`
	Error         string `json:"error,omitempty"`
}
