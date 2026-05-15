# Obot Scan: Top K Prompts

### Overview

Obot admins should be able to see a list of the top highest token usage prompts for scans submitted by users with the `obot scan` command.
These should be visible in the UI along with scan results and Admins/Owners/auditors should be able to see the: 
- prompt
- total input tokens
- total output tokens
- all other pertent information

They should also be able to "drill into" each of these prompts to see the breakdowns of the tool calls, agents, etc triggered by the prompt that are attributed to the totals.

We're going for a similar experience to https://github.com/matt1398/claude-devtools. You may want to look at how this is implemented to understand how to parse and work with the claude code session logs in particular. A local fork is available for you to paruse (see ~/projects/njhale/)

This also means that there should be a `--include-top-prompts <n>` optional flag added to `obot scan` that, when set, searches client logs to gather the appropriate info from available/supported clients.

### First Milestone Goal

- UI with top prompts and details page with breakdown when available
- At first we only want to support claude code
- CLI extended with new flag
