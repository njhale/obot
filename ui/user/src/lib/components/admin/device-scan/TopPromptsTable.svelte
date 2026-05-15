<script lang="ts">
	import { resolve } from '$app/paths';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import { formatNumber } from '$lib/format';
	import type {
		DeviceScanPrompt,
		DeviceScanPromptSubagent,
		DeviceScanPromptToolCall
	} from '$lib/services/admin/types';
	import { formatTimeAgo } from '$lib/time';
	import { openUrl } from '$lib/utils';

	type Props = {
		prompts: DeviceScanPrompt[];
		deviceId: string;
	};

	let { prompts, deviceId }: Props = $props();

	type Row = {
		id: string;
		chunkID: string;
		prompt_preview: string;
		prompt_full: string;
		started_at: string;
		started_relative: string;
		started_full: string;
		model: string;
		input_tokens: number;
		output_tokens: number;
		cache_read_tokens: number;
		cache_creation_tokens: number;
		total_tokens: number;
		tool_calls_count: number;
		subagents_count: number;
	};

	function countToolCalls(tc?: DeviceScanPromptToolCall[]): number {
		if (!tc) return 0;
		return tc.reduce((acc, t) => acc + (t.count ?? 0), 0);
	}

	function sumSubagentToolCalls(s?: DeviceScanPromptSubagent[]): number {
		if (!s) return 0;
		let total = 0;
		for (const node of s) {
			total += countToolCalls(node.toolCalls);
			total += sumSubagentToolCalls(node.subagents);
		}
		return total;
	}

	function countSubagents(s?: DeviceScanPromptSubagent[]): number {
		if (!s) return 0;
		let total = 0;
		for (const node of s) {
			total += 1 + countSubagents(node.subagents);
		}
		return total;
	}

	function previewText(text?: string): string {
		if (!text) return '—';
		const collapsed = text.replace(/\s+/g, ' ').trim();
		return collapsed.length > 140 ? collapsed.slice(0, 140) + '…' : collapsed;
	}

	let rows = $derived<Row[]>(
		prompts.map((p, i) => {
			const started = formatTimeAgo(p.startedAt);
			return {
				id: `${i}-${p.chunkID}`,
				chunkID: p.chunkID,
				prompt_preview: previewText(p.promptText),
				prompt_full: p.promptText ?? '',
				started_at: p.startedAt,
				started_relative: started.relativeTime,
				started_full: started.fullDate,
				model: p.model || '—',
				input_tokens: p.metrics?.inputTokens ?? 0,
				output_tokens: p.metrics?.outputTokens ?? 0,
				cache_read_tokens: p.metrics?.cacheReadTokens ?? 0,
				cache_creation_tokens: p.metrics?.cacheCreationTokens ?? 0,
				total_tokens: p.metrics?.totalTokens ?? 0,
				tool_calls_count: countToolCalls(p.toolCalls) + sumSubagentToolCalls(p.subagents),
				subagents_count: countSubagents(p.subagents)
			};
		})
	);

	let maxTokens = $derived(rows.reduce((m, r) => Math.max(m, r.total_tokens), 0));

	function pct(part: number, total: number): number {
		if (total <= 0) return 0;
		return Math.max(0, Math.min(100, (part / total) * 100));
	}
</script>

<Table
	data={rows}
	fields={[
		'prompt_preview',
		'started_relative',
		'model',
		'total_tokens',
		'tool_calls_count',
		'subagents_count'
	]}
	headers={[
		{ title: 'Prompt', property: 'prompt_preview' },
		{ title: 'Started', property: 'started_relative' },
		{ title: 'Model', property: 'model' },
		{ title: 'Tokens', property: 'total_tokens' },
		{ title: 'Tools', property: 'tool_calls_count' },
		{ title: 'Subagents', property: 'subagents_count' }
	]}
	sortable={['model', 'total_tokens', 'tool_calls_count', 'subagents_count', 'started_relative']}
	onClickRow={(d, isCtrlClick) => {
		openUrl(
			resolve(`/admin/devices/${deviceId}/prompts/${encodeURIComponent(d.chunkID)}`),
			isCtrlClick
		);
	}}
>
	{#snippet onRenderColumn(property, d: Row)}
		{#if property === 'prompt_preview'}
			<span class="font-mono text-xs" use:tooltip={d.prompt_full || 'no prompt text'}>
				{d.prompt_preview}
			</span>
		{:else if property === 'started_relative'}
			<span use:tooltip={d.started_full}>{d.started_relative || '—'}</span>
		{:else if property === 'model'}
			<span class="font-mono text-xs">{d.model}</span>
		{:else if property === 'total_tokens'}
			<div class="flex w-40 flex-col gap-1">
				<span class="font-mono text-xs">{formatNumber(d.total_tokens)}</span>
				<div
					class="flex h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700"
					use:tooltip={`input ${formatNumber(d.input_tokens)} · output ${formatNumber(
						d.output_tokens
					)} · cache_read ${formatNumber(d.cache_read_tokens)} · cache_creation ${formatNumber(
						d.cache_creation_tokens
					)}`}
				>
					<span class="bg-primary" style:width={`${pct(d.input_tokens, maxTokens)}%`}></span>
					<span class="bg-blue-500" style:width={`${pct(d.output_tokens, maxTokens)}%`}></span>
					<span class="bg-emerald-500" style:width={`${pct(d.cache_read_tokens, maxTokens)}%`}>
					</span>
					<span class="bg-amber-500" style:width={`${pct(d.cache_creation_tokens, maxTokens)}%`}>
					</span>
				</div>
			</div>
		{:else if property === 'tool_calls_count'}
			{d.tool_calls_count}
		{:else if property === 'subagents_count'}
			{d.subagents_count}
		{:else}
			{d[property as keyof Row] ?? '—'}
		{/if}
	{/snippet}
</Table>
