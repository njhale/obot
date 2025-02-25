package types

type Trigger struct {
	Metadata
	TriggerManifest
	TaskID string `json:"taskId"`
}

type TriggerManifest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Provider    string  `json:"provider"`
	Options     *string `json:"options,omitempty"`
}

type TriggerList List[Trigger]
