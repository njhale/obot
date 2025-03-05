package types

type ProviderTrigger struct {
	Metadata
	ProviderTriggerManifest
	TaskID string `json:"taskID"`
}

type ProviderTriggerManifest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Provider    string  `json:"provider"`
	Options     *string `json:"options,omitempty"`
}

type ProviderTriggerList List[ProviderTrigger]
