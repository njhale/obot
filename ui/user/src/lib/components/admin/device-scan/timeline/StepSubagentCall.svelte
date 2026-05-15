<script lang="ts">
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { formatNumber } from '$lib/format';
	import type {
		DeviceScanPrompt,
		DeviceScanPromptStep,
		DeviceScanPromptSubagent
	} from '$lib/services/admin/types';
	import MetricsPill from './MetricsPill.svelte';
	import Timeline from './Timeline.svelte';
	import { previewLine } from './textHelpers';
	import { ChevronDown, ChevronRight, GitBranch } from 'lucide-svelte';

	type Props = {
		step: DeviceScanPromptStep;
		prompt: DeviceScanPrompt;
		node?: DeviceScanPromptSubagent;
	};

	let { step, prompt, node }: Props = $props();

	let open = $state(false);

	function toggle() {
		open = !open;
	}

	let label = $derived(node?.subagentType || 'subagent');
	let description = $derived(node?.description ?? step.textHead ?? '');
	let preview = $derived(previewLine(description, 80));
	let impact = $derived(node?.mainSessionImpact);
	let impactTokens = $derived(
		impact
			? {
					input: impact.callTokens,
					output: impact.resultTokens,
					cacheRead: 0,
					cacheCreation: 0
				}
			: step.tokens
	);
</script>

<div
	class="border-surface2 dark:border-surface3 flex flex-col gap-2 rounded-md border border-dashed p-2"
>
	<button
		type="button"
		class="flex w-full items-start gap-2 text-left"
		onclick={toggle}
		aria-expanded={open}
	>
		{#if open}
			<ChevronDown class="mt-0.5 size-3.5 shrink-0 opacity-60" />
		{:else}
			<ChevronRight class="mt-0.5 size-3.5 shrink-0 opacity-60" />
		{/if}
		<GitBranch class="mt-0.5 size-3.5 shrink-0 opacity-70" />
		<span class="shrink-0 font-mono text-xs font-semibold">{label}</span>
		<span class="min-w-0 flex-1 truncate font-mono text-[11px]">
			{preview || '(no description)'}
		</span>
		<span
			class="text-on-surface1 shrink-0 rounded bg-purple-100 px-1.5 py-0.5 text-[10px] font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300"
			use:tooltip={'Token cost the parent paid to invoke this subagent.'}
		>
			impact
		</span>
		<MetricsPill tokens={impactTokens} />
	</button>

	{#if open}
		<div class="flex flex-col gap-2 pl-6">
			{#if node}
				<dl class="text-on-surface1 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-[10px]">
					{#if node.metrics}
						<dt>Internal total</dt>
						<dd class="font-mono">{formatNumber(node.metrics.totalTokens ?? 0)}</dd>
					{/if}
					{#if impact}
						<dt>Call tokens</dt>
						<dd class="font-mono">{formatNumber(impact.callTokens)}</dd>
						<dt>Result tokens</dt>
						<dd class="font-mono">{formatNumber(impact.resultTokens)}</dd>
					{/if}
				</dl>

				{#if step.subagentID}
					<div class="border-surface2 dark:border-surface3 border-l-2 pl-2">
						<Timeline {prompt} subagentID={step.subagentID} embedded />
					</div>
				{:else}
					<p class="text-on-surface1 text-[10px] italic">
						No nested timeline available for this subagent.
					</p>
				{/if}
			{:else}
				<p class="text-on-surface1 text-[10px] italic">
					Subagent node not found in tree (legacy row or unresolved id).
				</p>
			{/if}
		</div>
	{/if}
</div>
