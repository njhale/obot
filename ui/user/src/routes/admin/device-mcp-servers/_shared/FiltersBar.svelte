<script lang="ts">
	import { popover } from '$lib/actions';
	import { Filter, X } from 'lucide-svelte';

	interface Props {
		name: string;
		command: string;
		url: string;
		transports: string[];
		clients: string[];
		transportOptions: string[];
		clientOptions: string[];
		onChange: (next: {
			name: string;
			command: string;
			url: string;
			transports: string[];
			clients: string[];
		}) => void;
	}

	let {
		name,
		command,
		url,
		transports,
		clients,
		transportOptions,
		clientOptions,
		onChange
	}: Props = $props();

	const { ref, tooltip, toggle } = popover({ placement: 'bottom-start' });

	function emit(
		patch: Partial<Pick<Props, 'name' | 'command' | 'url' | 'transports' | 'clients'>>
	) {
		onChange({
			name,
			command,
			url,
			transports,
			clients,
			...patch
		});
	}

	function toggleArrayValue(arr: string[], v: string) {
		return arr.includes(v) ? arr.filter((x) => x !== v) : [...arr, v];
	}

	let activeCount = $derived(
		(name ? 1 : 0) + (command ? 1 : 0) + (url ? 1 : 0) + transports.length + clients.length
	);
</script>

<button
	type="button"
	use:ref
	onclick={() => toggle()}
	class="dark:border-surface3 dark:bg-surface1 hover:bg-surface1 dark:hover:bg-surface2 bg-background flex h-10 items-center gap-2 rounded-lg border border-transparent px-3 text-sm shadow-sm"
>
	<Filter class="size-4" />
	Filters{#if activeCount > 0}
		<span class="bg-primary text-on-primary rounded-full px-1.5 text-xs">{activeCount}</span>
	{/if}
</button>

<div use:tooltip class="popover z-50 flex w-80 flex-col gap-3 p-4">
	<div class="flex items-center justify-between">
		<h4 class="text-sm font-semibold">Filters</h4>
		{#if activeCount > 0}
			<button
				type="button"
				class="text-on-surface2 flex items-center gap-1 text-xs hover:underline"
				onclick={() => onChange({ name: '', command: '', url: '', transports: [], clients: [] })}
			>
				<X class="size-3" /> Clear all
			</button>
		{/if}
	</div>

	<label class="flex flex-col gap-1 text-xs">
		<span class="text-on-surface1">Name contains</span>
		<input
			type="text"
			class="dark:bg-surface2 bg-background dark:border-surface3 rounded border px-2 py-1 text-sm"
			value={name}
			oninput={(e) => emit({ name: (e.target as HTMLInputElement).value })}
		/>
	</label>
	<label class="flex flex-col gap-1 text-xs">
		<span class="text-on-surface1">Command contains</span>
		<input
			type="text"
			class="dark:bg-surface2 bg-background dark:border-surface3 rounded border px-2 py-1 text-sm"
			value={command}
			oninput={(e) => emit({ command: (e.target as HTMLInputElement).value })}
		/>
	</label>
	<label class="flex flex-col gap-1 text-xs">
		<span class="text-on-surface1">URL contains</span>
		<input
			type="text"
			class="dark:bg-surface2 bg-background dark:border-surface3 rounded border px-2 py-1 text-sm"
			value={url}
			oninput={(e) => emit({ url: (e.target as HTMLInputElement).value })}
		/>
	</label>

	<div class="flex flex-col gap-1 text-xs">
		<span class="text-on-surface1">Transport</span>
		<div class="flex flex-wrap gap-1">
			{#if transportOptions.length === 0}
				<span class="text-on-surface2 italic">No values yet</span>
			{:else}
				{#each transportOptions as opt (opt)}
					{@const selected = transports.includes(opt)}
					<button
						type="button"
						class="rounded-full border px-2 py-0.5 text-xs {selected
							? 'border-primary bg-primary text-on-primary'
							: 'dark:border-surface3 hover:bg-surface3 border-transparent'}"
						onclick={() => emit({ transports: toggleArrayValue(transports, opt) })}
					>
						{opt}
					</button>
				{/each}
			{/if}
		</div>
	</div>

	<div class="flex flex-col gap-1 text-xs">
		<span class="text-on-surface1">Client</span>
		<div class="flex flex-wrap gap-1">
			{#if clientOptions.length === 0}
				<span class="text-on-surface2 italic">No values yet</span>
			{:else}
				{#each clientOptions as opt (opt)}
					{@const selected = clients.includes(opt)}
					<button
						type="button"
						class="rounded-full border px-2 py-0.5 text-xs {selected
							? 'border-primary bg-primary text-on-primary'
							: 'dark:border-surface3 hover:bg-surface3 border-transparent'}"
						onclick={() => emit({ clients: toggleArrayValue(clients, opt) })}
					>
						{opt}
					</button>
				{/each}
			{/if}
		</div>
	</div>
</div>
