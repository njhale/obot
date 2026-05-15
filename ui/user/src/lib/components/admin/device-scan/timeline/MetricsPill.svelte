<script lang="ts">
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { formatNumber } from '$lib/format';
	import type { DeviceScanPromptStepTokens } from '$lib/services/admin/types';

	type Props = {
		tokens?: DeviceScanPromptStepTokens;
		accumulated?: number;
	};

	let { tokens, accumulated }: Props = $props();

	let input = $derived(tokens?.input ?? 0);
	let output = $derived(tokens?.output ?? 0);
	let cacheRead = $derived(tokens?.cacheRead ?? 0);
	let cacheCreation = $derived(tokens?.cacheCreation ?? 0);
	let total = $derived(input + output + cacheRead + cacheCreation);
	let max = $derived(Math.max(input, output, cacheRead, cacheCreation, 1));

	function pct(part: number): number {
		if (max <= 0) return 0;
		return Math.max(0, Math.min(100, (part / max) * 100));
	}

	let tipText = $derived(
		`input ${formatNumber(input)} · output ${formatNumber(output)} · cache_read ${formatNumber(
			cacheRead
		)} · cache_creation ${formatNumber(cacheCreation)}${
			accumulated !== undefined ? ` · ctx ${formatNumber(accumulated)}` : ''
		}`
	);
</script>

<span
	class="border-surface2 dark:border-surface3 inline-flex items-center gap-1.5 rounded border px-1.5 py-0.5 font-mono text-[10px]"
	use:tooltip={tipText}
>
	<span class="flex h-2 w-12 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
		<span class="bg-primary" style:width={`${pct(input)}%`}></span>
		<span class="bg-blue-500" style:width={`${pct(output)}%`}></span>
		<span class="bg-emerald-500" style:width={`${pct(cacheRead)}%`}></span>
		<span class="bg-amber-500" style:width={`${pct(cacheCreation)}%`}></span>
	</span>
	<span class="tabular-nums">{formatNumber(total)}</span>
	{#if accumulated !== undefined && accumulated > 0}
		<span class="text-on-surface1">|</span>
		<span class="text-on-surface1 tabular-nums">{formatNumber(accumulated)}</span>
	{/if}
</span>
