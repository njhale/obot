<script lang="ts">
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { toolNameIssues, toolNameSeverity, type ToolNameIssue } from '$lib/services/chat/mcp';
	import { AlertTriangle } from 'lucide-svelte';
	import { twMerge } from 'tailwind-merge';

	interface Props {
		effectiveName: string;
		// Additional issues (e.g. cross-component name conflicts) that callers
		// compute with broader context than this component has access to.
		extraIssues?: ToolNameIssue[];
		// Render the tooltip inline instead of portaling to document.body.
		// Required when the icon lives inside a native <dialog> modal, whose
		// top layer hides body-portaled tooltips behind the backdrop.
		disablePortal?: boolean;
		class?: string;
	}

	let { effectiveName, extraIssues, disablePortal = false, class: klass }: Props = $props();

	let issues = $derived([...toolNameIssues(effectiveName), ...(extraIssues ?? [])]);
	let severity = $derived(toolNameSeverity(issues));
</script>

{#if severity}
	{@const messages = issues.map((i) => i.message).join('\n\n')}
	<span
		class={twMerge(
			'inline-flex items-center',
			severity === 'error' ? 'text-red-500' : 'text-amber-500',
			klass
		)}
		use:tooltip={{ text: messages, placement: 'top', disablePortal }}
		aria-label={messages}
	>
		<AlertTriangle class="size-4 flex-shrink-0" />
	</span>
{/if}
