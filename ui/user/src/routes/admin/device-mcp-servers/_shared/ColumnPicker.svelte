<script lang="ts">
	import { popover } from '$lib/actions';
	import { Columns3 } from 'lucide-svelte';

	interface Column {
		id: string;
		label: string;
		alwaysVisible?: boolean;
	}

	interface Props {
		columns: Column[];
		visible: string[];
		onChange: (visible: string[]) => void;
	}

	let { columns, visible, onChange }: Props = $props();
	const { ref, tooltip, toggle } = popover({ placement: 'bottom-end' });

	function flip(id: string) {
		const next = visible.includes(id) ? visible.filter((x) => x !== id) : [...visible, id];
		onChange(next);
	}
</script>

<button
	type="button"
	use:ref
	onclick={() => toggle()}
	class="dark:border-surface3 dark:bg-surface1 hover:bg-surface1 dark:hover:bg-surface2 bg-background flex h-10 items-center gap-2 rounded-lg border border-transparent px-3 text-sm shadow-sm"
>
	<Columns3 class="size-4" />
	Columns
</button>

<div use:tooltip class="popover z-50 flex flex-col py-2">
	<div class="text-on-surface2 px-3 pt-1 pb-2 text-xs uppercase">Visible columns</div>
	{#each columns as col (col.id)}
		<label
			class="hover:bg-surface3/25 flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm {col.alwaysVisible
				? 'cursor-not-allowed opacity-60'
				: ''}"
		>
			<input
				type="checkbox"
				checked={col.alwaysVisible || visible.includes(col.id)}
				disabled={col.alwaysVisible}
				onchange={() => !col.alwaysVisible && flip(col.id)}
			/>
			<span>{col.label}</span>
		</label>
	{/each}
</div>
