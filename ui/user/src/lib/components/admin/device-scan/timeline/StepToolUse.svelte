<script lang="ts">
	import type { DeviceScanPromptStep } from '$lib/services/admin/types';
	import MetricsPill from './MetricsPill.svelte';
	import StepToolResult from './StepToolResult.svelte';
	import { ChevronDown, ChevronRight, Wrench } from 'lucide-svelte';

	type Props = {
		step: DeviceScanPromptStep;
		result?: DeviceScanPromptStep;
	};

	let { step, result }: Props = $props();

	let open = $state(false);

	function toggle() {
		open = !open;
	}

	let keys = $derived(step.toolInputKeys ?? []);
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
		<Wrench class="mt-0.5 size-3.5 shrink-0 opacity-70" />
		<span class="shrink-0 font-mono text-xs font-semibold">{step.toolName || 'tool'}</span>
		{#if keys.length > 0}
			<span class="text-on-surface1 min-w-0 flex-1 truncate font-mono text-[11px]">
				{keys.join(', ')}
			</span>
		{:else}
			<span class="min-w-0 flex-1"></span>
		{/if}
		{#if result?.isError}
			<span
				class="shrink-0 rounded bg-red-100 px-1.5 py-0.5 text-[10px] font-medium text-red-700 dark:bg-red-900/40 dark:text-red-300"
				>error</span
			>
		{/if}
		<MetricsPill tokens={step.tokens} accumulated={step.accumulatedContextTokens} />
	</button>

	{#if open}
		<div class="flex flex-col gap-2 pl-6">
			{#if keys.length > 0}
				<div class="flex flex-col gap-1">
					<span class="text-on-surface1 text-[10px] font-medium tracking-wide uppercase">
						Input keys
					</span>
					<div class="flex flex-wrap gap-1">
						{#each keys as k (k)}
							<span
								class="bg-surface1 dark:bg-surface3 rounded px-1.5 py-0.5 font-mono text-[10px]"
							>
								{k}
							</span>
						{/each}
					</div>
					<span class="text-on-surface1 text-[10px]">(values redacted)</span>
				</div>
			{:else}
				<span class="text-on-surface1 text-[10px]">(no input keys recorded)</span>
			{/if}

			{#if step.toolUseID}
				<dl class="text-on-surface1 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-[10px]">
					<dt>Tool use id</dt>
					<dd class="font-mono break-all">{step.toolUseID}</dd>
				</dl>
			{/if}

			{#if result}
				<div class="border-surface2 dark:border-surface3 mt-1 border-l-2 pl-2">
					<StepToolResult step={result} embedded />
				</div>
			{/if}
		</div>
	{/if}
</div>
