<script lang="ts">
	import type { Task, TriggerProvider } from '$lib/services';
	import { ChatService } from '$lib/services';
	import { onMount } from 'svelte';

	interface Props {
		task: Task;
		editMode?: boolean;
		onChanged?: (task: Task) => void | Promise<void>;
	}

	let { task, editMode = false, onChanged }: Props = $props();
	let providers: TriggerProvider[] = $state([]);

	onMount(async () => {
		providers = await ChatService.listTriggerProviders();
	});

	async function updateProvider(provider: string) {
		await onChanged?.({
			...task,
			byTriggerProvider: {
				...(task.byTriggerProvider || {}),
				provider
			}
		});
	}

	async function updateOptions(options: string) {
		await onChanged?.({
			...task,
			byTriggerProvider: {
				...(task.byTriggerProvider || {}),
				provider: task.byTriggerProvider?.provider || '',
				options
			}
		});
	}
</script>

<div class="flex flex-col gap-4">
	<h3 class="text-lg font-semibold">By Trigger Provider</h3>
	{#if editMode}
		<div>
			<label for="provider" class="text-sm font-medium">Provider</label>
			<select
				id="provider"
				class="mt-1 w-full rounded-md border p-2"
				value={task.byTriggerProvider?.provider ?? ''}
				on:change={(e) => updateProvider(e.currentTarget.value)}
			>
				<option value="">Select a provider</option>
				{#each providers as provider}
					<option value={provider.name}>{provider.name}</option>
				{/each}
			</select>
		</div>
		<div>
			<label for="options" class="text-sm font-medium">Provider Options</label>
			<textarea
				id="options"
				class="mt-1 w-full rounded-md border p-2"
				placeholder="Provider options (JSON)"
				value={task.byTriggerProvider?.options ?? ''}
				on:input={(e) => updateOptions(e.currentTarget.value)}
			></textarea>
		</div>
	{:else if task.byTriggerProvider}
		<div class="text-sm">
			<div>Provider: {task.byTriggerProvider.provider}</div>
			{#if task.byTriggerProvider.options}
				<div>Options: {task.byTriggerProvider.options}</div>
			{/if}
		</div>
	{/if}
</div>
