---
title: Tool Confirmation
---

# Tool Confirmation

When MCP server tools are called during a chat, users must approve each tool call before execution. This ensures users maintain control over what actions are performed.

## Approval Options

When a tool call requires approval, an approval bar appears showing the tool name and an option to view input details. Users can:

| Option | Description | Scope |
|--------|-------------|-------|
| **Deny** | Reject this tool call | Single call |
| **Allow** | Approve this tool call | Single call |
| **Allow all [Tool] requests** | Pre-approve this specific tool | Thread duration |
| **Allow all [Server] requests** | Pre-approve all tools from an MCP server | Thread duration |
| **Allow all requests** | Pre-approve all tool calls | Thread duration |

Thread-scoped approvals reset when starting a new thread.

## Automatic Approval

Tool calls execute automatically without prompts when:

- Running system tasks or workflows
- Using Obot's built-in tools
- The administrator has enabled autonomous tool use platform-wide
- The user has enabled "Allow Autonomous Tool Use" in their profile
- The user has pre-approved the tool using "Allow all" options

## User Settings

To enable automatic tool execution for all your chat sessions:

1. Navigate to **Profile > My Account**
2. Enable **Allow Autonomous Tool Use**

This setting only appears if the administrator has not globally enabled autonomous tool use.

## Administrator Configuration

Administrators can disable tool call approval platform-wide by setting:

```
OBOT_SERVER_ENABLE_AUTONOMOUS_TOOL_USE=true
```

When enabled, all chat sessions run tools automatically without user approval prompts, and the user toggle is hidden.

See [Server Configuration](/configuration/server-configuration/) for all environment variables.
