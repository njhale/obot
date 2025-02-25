package types

type TriggerProvider struct {
	Metadata
	TriggerProviderManifest
	TriggerProviderStatus
}

type TriggerProviderManifest struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	ToolReference string `json:"toolReference"`
}

type TriggerProviderStatus struct {
	CommonProviderMetadata
	Configured                      bool                             `json:"configured"`
	ObotScopes                      []string                         `json:"obotScopes,omitempty"`
	RequiredConfigurationParameters []ProviderConfigurationParameter `json:"requiredConfigurationParameters,omitempty"`
	OptionalConfigurationParameters []ProviderConfigurationParameter `json:"optionalConfigurationParameters,omitempty"`
	MissingConfigurationParameters  []string                         `json:"missingConfigurationParameters,omitempty"`
}

type TriggerProviderList List[TriggerProvider]
