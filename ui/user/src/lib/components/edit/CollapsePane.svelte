<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import { ChevronDown } from 'lucide-svelte/icons';
	import { twMerge } from 'tailwind-merge';
	import { slide } from 'svelte/transition';

	interface Props {
		header: string | Snippet;
		children: Snippet;
		open?: boolean;
		onOpen?: () => void | Promise<void>;
		classes?: { header?: string; content?: string };
		showDropdown?: boolean;
	}

	onMount(() => {
		if (open) {
			onOpen?.();
		}
	});

	let {
		header,
		children,
		open = $bindable(false),
		onOpen,
		classes = {},
		showDropdown = true
	}: Props = $props();
</script>

<div class="flex flex-col">
	{#if header}
		<button
			class={twMerge('flex items-center gap-2 px-5 py-2', classes.header)}
			onclick={() => {
				if (!open) {
					onOpen?.();
				}
				open = !open;
			}}
		>
			{#if typeof header === 'string'}
				<span class="grow text-start text-base font-extralight">
					{header}
				</span>
			{:else}
				{@render header()}
			{/if}

			{#if showDropdown}
				<span class:rotate-180={open} class="transition-transform duration-200">
					<ChevronDown />
				</span>
			{/if}
		</button>
	{/if}
	{#if open}
		<div
			transition:slide
			class={twMerge('border-surface1 bg-surface2 flex flex-col p-5 shadow-inner', classes.content)}
		>
			{@render children()}
		</div>
	{/if}
</div>
