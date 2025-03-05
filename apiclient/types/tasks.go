package types

type Task struct {
	Metadata
	TaskManifest
	ThreadID string `json:"threadID,omitempty"`
	// TODO(njhale): Ensure we need this field.
	ProjectID string `json:"projectID"`
}

type TaskList List[Task]

type TaskManifest struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Steps             []TaskStep             `json:"steps"`
	Schedule          *Schedule              `json:"schedule"`
	Webhook           *TaskWebhook           `json:"webhook"`
	Email             *TaskEmail             `json:"email"`
	OnDemand          *TaskOnDemand          `json:"onDemand"`
	ByTriggerProvider *TaskByTriggerProvider `json:"byTriggerProvider"`
}

type TaskOnDemand struct {
	Params map[string]string `json:"params,omitempty"`
}

type TaskWebhook struct {
}

type TaskEmail struct {
}

// TODO(njhale): The options will be a JSON string that the UI will know how to build.
// 	For now, when it sees that the "slack-trigger-provider" is enabled, it will show the options to configure the trigger.
// 	To support type-ahead on the field, it will make a tool call to the provider to get the top-k channels and users that match the partially completed strings until one is selected.

type TaskByTriggerProvider struct {
	Provider string  `json:"provider"`
	Options  *string `json:"options,omitempty"`
}

type Schedule struct {
	// Valid values are: "hourly", "daily", "weekly", "monthly"
	Interval string `json:"interval"`
	Hour     int    `json:"hour"`
	Minute   int    `json:"minute"`
	Day      int    `json:"day"`
	Weekday  int    `json:"weekday"`
}

type TaskStep struct {
	ID   string `json:"id,omitempty"`
	Step string `json:"step,omitempty"`
}

type TaskRun struct {
	Metadata
	TaskID    string       `json:"taskID,omitempty"`
	Input     string       `json:"input,omitempty"`
	Task      TaskManifest `json:"task,omitempty"`
	StartTime *Time        `json:"startTime,omitempty"`
	EndTime   *Time        `json:"endTime,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type TaskRunList List[TaskRun]
