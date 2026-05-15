<script lang="ts">
	import { resolve } from '$app/paths';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import CopyButton from '$lib/components/CopyButton.svelte';
	import Layout from '$lib/components/Layout.svelte';
	import Timeline from '$lib/components/admin/device-scan/timeline/Timeline.svelte';
	import { PAGE_TRANSITION_DURATION } from '$lib/constants';
	import { formatNumber } from '$lib/format';
	import type { DeviceScanPrompt, DeviceScanPromptMetrics } from '$lib/services/admin/types';
	import { formatTimeAgo } from '$lib/time';
	import { ChevronLeft } from 'lucide-svelte';
	import { fly } from 'svelte/transition';

	let { data } = $props();
	let prompt = $derived<DeviceScanPrompt | undefined>(data?.prompt);
	let deviceId = $derived<string>(data?.deviceId ?? '');

	const duration = PAGE_TRANSITION_DURATION;

	let startedTime = $derived(
		prompt ? formatTimeAgo(prompt.startedAt) : { relativeTime: '', fullDate: '' }
	);
	let endedTime = $derived(
		prompt ? formatTimeAgo(prompt.endedAt) : { relativeTime: '', fullDate: '' }
	);

	function formatDuration(ms: number): string {
		if (!Number.isFinite(ms) || ms <= 0) return '—';
		const totalSec = Math.round(ms / 1000);
		const min = Math.floor(totalSec / 60);
		const sec = totalSec % 60;
		if (min === 0) return `${sec}s`;
		return `${min}m ${sec}s`;
	}

	let subagentExtra = $derived<number>(
		prompt ? Math.max(0, prompt.metrics.totalTokens - prompt.mainMetrics.totalTokens) : 0
	);
</script>

<Layout>
	<div
		class="flex flex-col gap-6"
		in:fly={{ x: 100, duration, delay: duration }}
		out:fly={{ x: -100, duration }}
	>
		{#if !prompt}
			<p class="text-on-surface1 text-sm font-light">Prompt not found.</p>
		{:else}
			<a
				class="btn-link text-on-surface1 inline-flex w-fit items-center gap-1 text-sm"
				href={resolve(`/admin/devices/${deviceId}`)}
			>
				<ChevronLeft class="size-4" />
				Back to device
			</a>

			<!-- Header -->
			<div class="dark:bg-surface2 bg-background flex flex-col gap-3 rounded-md p-4 shadow-sm">
				<div class="flex items-start justify-between gap-3">
					<h2 class="text-base font-semibold">Prompt</h2>
					<CopyButton text={prompt.promptText ?? ''} tooltipText="Copy prompt" />
				</div>
				<pre
					class="bg-surface1 dark:bg-surface3 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">{prompt.promptText ||
						'(no prompt text captured)'}</pre>
				<dl class="text-on-surface1 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-xs">
					<dt>SHA-256</dt>
					<dd class="font-mono break-all">{prompt.promptHash}</dd>
					<dt>Full length</dt>
					<dd class="font-mono">{formatNumber(prompt.promptBytes)} bytes</dd>
				</dl>

				<div class="border-surface1 dark:border-surface3 border-t pt-3">
					<dl class="grid grid-cols-[max-content_1fr] items-center gap-x-4 gap-y-2 text-sm">
						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Client</dt>
						<dd class="font-mono text-xs">{prompt.client}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Model</dt>
						<dd class="font-mono text-xs">{prompt.model || '—'}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Session</dt>
						<dd class="font-mono text-xs">{prompt.sessionID}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Chunk</dt>
						<dd class="font-mono text-xs">{prompt.chunkID}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Cwd</dt>
						<dd class="font-mono text-xs">{prompt.cwd || '—'}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Branch</dt>
						<dd class="font-mono text-xs">{prompt.gitBranch || '—'}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Started</dt>
						<dd use:tooltip={startedTime.fullDate}>{startedTime.relativeTime || '—'}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Ended</dt>
						<dd use:tooltip={endedTime.fullDate}>{endedTime.relativeTime || '—'}</dd>

						<dt class="text-on-surface1 text-xs font-medium tracking-wide uppercase">Duration</dt>
						<dd class="font-mono text-xs">{formatDuration(prompt.durationMs)}</dd>
					</dl>
				</div>
			</div>

			<!-- Tokens -->
			<div class="dark:bg-surface2 bg-background flex flex-col gap-3 rounded-md p-4 shadow-sm">
				<div class="flex items-baseline gap-3">
					<h3 class="text-sm font-semibold">Tokens</h3>
					<span class="text-on-surface1 font-mono text-xs">
						{formatNumber(prompt.metrics.totalTokens)} total (incl. subagents)
					</span>
				</div>
				<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
					<div class="flex flex-col gap-1">
						<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">
							Parent context
						</h4>
						{@render metricsCard(prompt.mainMetrics)}
					</div>
					<div class="flex flex-col gap-1">
						<h4 class="text-on-surface1 text-xs font-medium tracking-wide uppercase">
							Transitive (incl. subagents)
						</h4>
						{@render metricsCard(prompt.metrics)}
					</div>
				</div>
				<p class="text-on-surface1 text-xs">
					Parent context saw <span class="font-mono"
						>{formatNumber(prompt.mainMetrics.totalTokens)}</span
					>; subagents consumed
					<span class="font-mono">{formatNumber(subagentExtra)}</span> extra internally.
				</p>
			</div>

			<!-- Timeline -->
			<div class="dark:bg-surface2 bg-background flex flex-col gap-2 rounded-md p-4 shadow-sm">
				<Timeline {prompt} />
			</div>
		{/if}
	</div>
</Layout>

{#snippet metricsCard(m: DeviceScanPromptMetrics)}
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
