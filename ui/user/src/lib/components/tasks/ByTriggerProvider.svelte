<script lang="ts">
	import type { Task } from '$lib/services';

	interface Props {
		task: Task;
		editMode?: boolean;
		onChanged?: (task: Task) => void | Promise<void>;
	}

	let { task, editMode = false, onChanged }: Props = $props();

	async function updateProvider(provider: string) {
		if (!task.byTriggerProvider) {
			task.byTriggerProvider = { provider };
		} else {
			task.byTriggerProvider.provider = provider;
		}
		await onChanged?.(task);
	}

	async function updateOptions(options: string) {
		if (!task.byTriggerProvider) {
			task.byTriggerProvider = { provider: '', options };
		} else {
			task.byTriggerProvider.options = options;
		}
		await onChanged?.(task);
	}
</script>

<div class="flex flex-col gap-4">
	<h3 class="text-lg font-semibold">By Trigger Provider</h3>
	{#if editMode}
		<div>
			<label for="provider" class="text-sm font-medium">Provider</label>
			<input
				id="provider"
				type="text"
				class="mt-1 w-full rounded-md border p-2"
				placeholder="Provider name"
				value={task.byTriggerProvider?.provider ?? ''}
				oninput={(e) => updateProvider(e.currentTarget.value)}
			/>
		</div>
		<div>
			<label for="options" class="text-sm font-medium">Provider Options</label>
			<textarea
				id="options"
				class="mt-1 w-full rounded-md border p-2"
				placeholder="Provider options (JSON)"
				value={task.byTriggerProvider?.options ?? ''}
				oninput={(e) => updateOptions(e.currentTarget.value)}
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
