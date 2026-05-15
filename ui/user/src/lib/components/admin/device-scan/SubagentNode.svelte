<script lang="ts">
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { formatNumber } from '$lib/format';
	import type {
		DeviceScanPromptMetrics,
		DeviceScanPromptSubagent
	} from '$lib/services/admin/types';
	import Self from './SubagentNode.svelte';
	import { ChevronDown, ChevronRight, GitBranch } from 'lucide-svelte';
	import { untrack } from 'svelte';

	type Props = {
		node: DeviceScanPromptSubagent;
		depth?: number;
		defaultOpen?: boolean;
	};

	let { node, depth = 0, defaultOpen = false }: Props = $props();

	let open = $state(untrack(() => defaultOpen));

	function toggle() {
		open = !open;
	}

	let internal = $derived<DeviceScanPromptMetrics>(node.metrics);
	let impact = $derived(node.mainSessionImpact);

	let toolRows = $derived(
		(node.toolCalls ?? []).slice().sort((a, b) => (b.count ?? 0) - (a.count ?? 0))
	);
	let children = $derived(node.subagents ?? []);
</script>

<div
	class="border-surface2 dark:border-surface2 flex flex-col gap-2 rounded-md border p-3"
	style:margin-left={`${Math.min(depth, 4) * 12}px`}
>
	<button
		type="button"
		class="flex items-start gap-2 text-left"
		onclick={toggle}
		aria-expanded={open}
	>
		{#if open}
			<ChevronDown class="mt-0.5 size-4 shrink-0 opacity-60" />
		{:else}
			<ChevronRight class="mt-0.5 size-4 shrink-0 opacity-60" />
		{/if}
		<div class="flex flex-1 flex-col gap-1">
			<div class="flex items-center gap-2">
				<GitBranch class="size-3.5 opacity-50" />
				<span class="font-mono text-xs font-semibold">
					{node.subagentType || 'subagent'}
				</span>
				<span class="text-on-surface1 text-xs">·</span>
				<span class="font-mono text-xs">
					{formatNumber(internal.totalTokens)} tok
				</span>
				{#if children.length > 0}
					<span class="text-on-surface1 text-xs"
						>· {children.length} child{children.length === 1 ? '' : 'ren'}</span
					>
				{/if}
			</div>
			{#if node.description}
				<p class="text-on-surface1 text-xs">{node.description}</p>
			{/if}
		</div>
	</button>

	{#if open}
		<div class="flex flex-col gap-3 pl-6">
			<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
				<div
					class="dark:bg-surface2 bg-background flex flex-col gap-1 rounded-md p-3"
					use:tooltip={'Token totals inside this subagent and its descendants.'}
				>
					<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">
						Internal metrics
					</h4>
					{@render metricsRows(internal)}
				</div>
				<div
					class="dark:bg-surface2 bg-background flex flex-col gap-1 rounded-md p-3"
					use:tooltip={'Token cost paid by the direct parent context to invoke this subagent.'}
				>
					<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">
						Main-session impact
					</h4>
					<dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
						<dt class="text-on-surface1">Call</dt>
						<dd class="font-mono">{formatNumber(impact.callTokens)}</dd>
						<dt class="text-on-surface1">Result</dt>
						<dd class="font-mono">{formatNumber(impact.resultTokens)}</dd>
						<dt class="text-on-surface1">Total</dt>
						<dd class="font-mono font-semibold">{formatNumber(impact.totalTokens)}</dd>
					</dl>
				</div>
			</div>

			{#if toolRows.length > 0}
				<div class="flex flex-col gap-1">
					<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Tool calls</h4>
					<table class="w-full text-xs">
						<thead class="text-on-surface1 text-left">
							<tr>
								<th class="py-1 pr-2 font-normal">Name</th>
								<th class="py-1 pr-2 text-right font-normal">Count</th>
							</tr>
						</thead>
						<tbody>
							{#each toolRows as t (t.name)}
								<tr class="border-surface2 border-t">
									<td class="py-1 pr-2 font-mono">{t.name}</td>
									<td class="py-1 pr-2 text-right font-mono">{t.count}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if children.length > 0}
				<div class="flex flex-col gap-2">
					<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">
						Subagents · {children.length}
					</h4>
					{#each children as child, i (i)}
						<Self node={child} depth={depth + 1} />
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>

{#snippet metricsRows(m: DeviceScanPromptMetrics)}
	<dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
		<dt class="text-on-surface1">Input</dt>
		<dd class="font-mono">{formatNumber(m.inputTokens)}</dd>
		<dt class="text-on-surface1">Output</dt>
		<dd class="font-mono">{formatNumber(m.outputTokens)}</dd>
		<dt class="text-on-surface1">Cache read</dt>
		<dd class="font-mono">{formatNumber(m.cacheReadTokens)}</dd>
		<dt class="text-on-surface1">Cache creation</dt>
		<dd class="font-mono">{formatNumber(m.cacheCreationTokens)}</dd>
		<dt class="text-on-surface1">Total</dt>
		<dd class="font-mono font-semibold">{formatNumber(m.totalTokens)}</dd>
	</dl>
{/snippet}
