<script lang="ts">
	import { formatNumber } from '$lib/format';
	import type { DeviceScanPromptStep } from '$lib/services/admin/types';
	import MetricsPill from './MetricsPill.svelte';
	import { previewLine } from './textHelpers';
	import { ChevronDown, ChevronRight, CircleAlert, CircleCheck } from 'lucide-svelte';

	type Props = {
		step: DeviceScanPromptStep;
		/** When true the component renders without an outer border (embedded under StepToolUse). */
		embedded?: boolean;
	};

	let { step, embedded = false }: Props = $props();

	let open = $state(false);

	function toggle() {
		open = !open;
	}

	let preview = $derived(previewLine(step.textHead ?? '', 100));
	let head = $derived(step.textHead ?? '');
	let outerClass = $derived(
		embedded
			? 'flex flex-col gap-1.5'
			: 'border-surface2 dark:border-surface3 flex flex-col gap-2 rounded-md border p-2'
	);
</script>

<div class={outerClass}>
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
		{#if step.isError}
			<CircleAlert class="mt-0.5 size-3.5 shrink-0 text-red-500" />
		{:else}
			<CircleCheck class="mt-0.5 size-3.5 shrink-0 text-emerald-500" />
		{/if}
		<span class="text-on-surface1 shrink-0 text-xs font-medium tracking-wide uppercase">
			{step.isError ? 'Tool error' : 'Tool result'}
		</span>
		<span class="min-w-0 flex-1 truncate font-mono text-xs">
			{preview || '(empty)'}
		</span>
		<MetricsPill tokens={step.tokens} accumulated={step.accumulatedContextTokens} />
	</button>

	{#if open}
		<div class="flex flex-col gap-2 pl-6">
			<pre
				class="bg-surface1 dark:bg-surface3 max-h-72 overflow-auto rounded-md p-2 font-mono text-[11px] whitespace-pre-wrap">{head ||
					'(empty)'}</pre>
			<dl class="text-on-surface1 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-[10px]">
				{#if step.textBytes}
					<dt>Full length</dt>
					<dd class="font-mono">{formatNumber(step.textBytes)} bytes</dd>
				{/if}
				{#if step.textHash}
					<dt>SHA-256</dt>
					<dd class="font-mono break-all">{step.textHash}</dd>
				{/if}
				{#if step.toolUseRef}
					<dt>Tool use ref</dt>
					<dd class="font-mono break-all">{step.toolUseRef}</dd>
				{/if}
			</dl>
		</div>
	{/if}
</div>
