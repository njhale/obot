<script lang="ts">
	import { formatNumber } from '$lib/format';
	import type { DeviceScanPromptStep } from '$lib/services/admin/types';
	import MetricsPill from './MetricsPill.svelte';
	import { previewLine } from './textHelpers';
	import { ChevronDown, ChevronRight, MessageSquare, User } from 'lucide-svelte';

	type Props = {
		step: DeviceScanPromptStep;
	};

	let { step }: Props = $props();

	let open = $state(false);

	function toggle() {
		open = !open;
	}

	let isUser = $derived(step.kind === 'user');
	let label = $derived(isUser ? 'User' : 'Assistant');
	let preview = $derived(previewLine(step.textHead ?? '', 100));
	let head = $derived(step.textHead ?? '');
</script>

<div class="border-surface2 dark:border-surface3 flex flex-col gap-2 rounded-md border p-2">
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
		{#if isUser}
			<User class="mt-0.5 size-3.5 shrink-0 opacity-70" />
		{:else}
			<MessageSquare class="mt-0.5 size-3.5 shrink-0 opacity-70" />
		{/if}
		<span class="text-on-surface1 shrink-0 text-xs font-medium tracking-wide uppercase">
			{label}
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
			</dl>
		</div>
	{/if}
</div>
