<script lang="ts">
	import type {
		DeviceScanPrompt,
		DeviceScanPromptStep,
		DeviceScanPromptStepTokens,
		DeviceScanPromptSubagent
	} from '$lib/services/admin/types';
	import MetricsPill from './MetricsPill.svelte';
	import StepSubagentCall from './StepSubagentCall.svelte';
	import StepText from './StepText.svelte';
	import StepThinking from './StepThinking.svelte';
	import StepToolResult from './StepToolResult.svelte';
	import StepToolUse from './StepToolUse.svelte';
	import { SvelteMap, SvelteSet } from 'svelte/reactivity';

	type Props = {
		prompt: DeviceScanPrompt;
		subagentID?: string;
		/** When true, render flat (no inner card chrome) — used for nested subagent views. */
		embedded?: boolean;
	};

	let { prompt, subagentID, embedded = false }: Props = $props();

	let allSteps = $derived<DeviceScanPromptStep[]>(prompt.steps ?? []);

	let filteredSteps = $derived<DeviceScanPromptStep[]>(
		subagentID === undefined
			? allSteps.filter((s) => s.context === 'main')
			: allSteps.filter((s) => s.context === 'subagent' && s.subagentID === subagentID)
	);

	let subagentIndex = $derived<Map<string, DeviceScanPromptSubagent>>(buildSubagentIndex(prompt));
	let toolResultIndex = $derived<Map<string, DeviceScanPromptStep>>(
		buildToolResultIndex(filteredSteps)
	);

	let consumedResultIds = $derived<Set<string>>(consumedResultIDs(filteredSteps, toolResultIndex));

	let totals = $derived<DeviceScanPromptStepTokens>(sumTokens(filteredSteps));

	function buildSubagentIndex(p: DeviceScanPrompt): Map<string, DeviceScanPromptSubagent> {
		const out = new SvelteMap<string, DeviceScanPromptSubagent>();
		function walk(nodes?: DeviceScanPromptSubagent[]) {
			if (!nodes) return;
			for (const n of nodes) {
				if (n.subagentID) out.set(n.subagentID, n);
				walk(n.subagents);
			}
		}
		walk(p.subagents);
		return out;
	}

	function buildToolResultIndex(steps: DeviceScanPromptStep[]): Map<string, DeviceScanPromptStep> {
		const out = new SvelteMap<string, DeviceScanPromptStep>();
		for (const s of steps) {
			if (s.kind === 'tool_result' && s.toolUseRef) {
				if (!out.has(s.toolUseRef)) out.set(s.toolUseRef, s);
			}
		}
		return out;
	}

	function consumedResultIDs(
		steps: DeviceScanPromptStep[],
		results: Map<string, DeviceScanPromptStep>
	): Set<string> {
		const ids = new SvelteSet<string>();
		for (const s of steps) {
			if (s.kind === 'tool_use' && s.toolUseID && results.has(s.toolUseID)) {
				ids.add(s.toolUseID);
			}
		}
		return ids;
	}

	function sumTokens(steps: DeviceScanPromptStep[]): DeviceScanPromptStepTokens {
		let input = 0;
		let output = 0;
		let cacheRead = 0;
		let cacheCreation = 0;
		for (const s of steps) {
			input += s.tokens?.input ?? 0;
			output += s.tokens?.output ?? 0;
			cacheRead += s.tokens?.cacheRead ?? 0;
			cacheCreation += s.tokens?.cacheCreation ?? 0;
		}
		return { input, output, cacheRead, cacheCreation };
	}

	function isOrphanedResult(step: DeviceScanPromptStep): boolean {
		if (step.kind !== 'tool_result') return false;
		if (!step.toolUseRef) return true;
		return !consumedResultIds.has(step.toolUseRef);
	}

	function maxAccumulated(steps: DeviceScanPromptStep[]): number {
		let m = 0;
		for (const s of steps) {
			if ((s.accumulatedContextTokens ?? 0) > m) m = s.accumulatedContextTokens ?? 0;
		}
		return m;
	}
</script>

<div class="flex flex-col gap-2" class:gap-3={!embedded}>
	{#if !embedded}
		<div class="flex items-center justify-between gap-3">
			<h3 class="text-sm font-semibold">
				Timeline{subagentID === undefined ? '' : ' · subagent'}
			</h3>
			{#if filteredSteps.length > 0}
				<MetricsPill tokens={totals} accumulated={maxAccumulated(filteredSteps)} />
			{/if}
		</div>
	{/if}

	{#if filteredSteps.length === 0}
		<p class="text-on-surface1 text-xs italic">
			{#if !prompt.steps || prompt.steps.length === 0}
				No timeline captured for this prompt.
			{:else}
				No steps in this context.
			{/if}
		</p>
	{:else}
		<div class="flex flex-col gap-1.5">
			{#each filteredSteps as step, i (i)}
				{#if step.kind === 'tool_result' && !isOrphanedResult(step)}
					<!-- skip: rendered inline under its matching tool_use -->
				{:else if step.kind === 'tool_use'}
					<StepToolUse
						{step}
						result={step.toolUseID ? toolResultIndex.get(step.toolUseID) : undefined}
					/>
				{:else if step.kind === 'tool_result'}
					<StepToolResult {step} />
				{:else if step.kind === 'thinking'}
					<StepThinking {step} />
				{:else if step.kind === 'text' || step.kind === 'user'}
					<StepText {step} />
				{:else if step.kind === 'subagent_call'}
					<StepSubagentCall
						{step}
						{prompt}
						node={step.subagentID ? subagentIndex.get(step.subagentID) : undefined}
					/>
				{/if}
			{/each}
		</div>
	{/if}
</div>
