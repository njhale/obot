package system

import "strings"

const (
	ThreadPrefix              = "t1"
	ThreadSharePrefix         = "ts1"
	ThreadAuthorizationPrefix = "ta1"
	AgentPrefix               = "a1"
	RunPrefix                 = "r1"
	ChatRunPrefix             = "r1chat"
	WorkflowPrefix            = "w1"
	WorkflowExecutionPrefix   = "we1"
	WorkflowStepPrefix        = "ws1"
	WorkspacePrefix           = "wksp1"
	WebhookPrefix             = "wh1"
	CronJobPrefix             = "cj1"
	KnowledgeSourcePrefix     = "ks1"
	OAuthAppPrefix            = "oa1"
	KnowledgeSetPrefix        = "kst1"
	OAuthAppLoginPrefix       = "oal1"
	EmailReceiverPrefix       = "er1"
	ModelPrefix               = "m1"
	AliasPrefix               = "al1"
	DefaultModelAliasPrefix   = "dma1"
	ToolPrefix                = "tl1"
	ProviderTriggerPrefix     = "ptr1"
	ProjectPrefix             = "p1"
	ThreadTemplatePrefix      = "tt1"
)

func IsThreadID(id string) bool {
	return strings.HasPrefix(id, ThreadPrefix)
}

func IsThreadTemplateID(id string) bool {
	return strings.HasPrefix(id, ThreadTemplatePrefix)
}

func IsToolID(id string) bool {
	return strings.HasPrefix(id, ToolPrefix)
}

func IsAgentID(id string) bool {
	return strings.HasPrefix(id, AgentPrefix)
}

func IsRunID(id string) bool {
	return strings.HasPrefix(id, RunPrefix)
}

func IsWebhookID(id string) bool {
	return strings.HasPrefix(id, WebhookPrefix)
}

func IsWorkflowID(id string) bool {
	return strings.HasPrefix(id, WorkflowPrefix)
}

func IsEmailReceiverID(id string) bool {
	return strings.HasPrefix(id, EmailReceiverPrefix)
}

func IsChatRunID(id string) bool {
	return strings.HasPrefix(id, ChatRunPrefix)
}
