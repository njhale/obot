<script lang="ts">
	import {
		ChatService,
		type Project,
		type ToolConfirm,
		type ToolConfirmDecision,
		type Message
	} from '$lib/services';
	import { slide } from 'svelte/transition';
	import popover from '$lib/actions/popover.svelte';
	import { ChevronDown, LoaderCircle } from 'lucide-svelte/icons';

	interface Props {
		messages: Message[];
		project: Project;
		currentThreadID: string;
	}

	let { messages, project, currentThreadID }: Props = $props();

	// Only track the first pending toolConfirm and its message, avoiding full array scans
	let current = $derived.by(() => {
		for (const msg of messages) {
			if (msg.toolConfirm && !msg.done) {
				return { confirm: msg.toolConfirm, message: msg };
			}
		}
		return undefined;
	});

	let displayName = $derived(current?.message.sourceName || current?.confirm.toolName || '');

	let isSubmitted = $state(false);
	let isExpanded = $state(false);

	// Reset state when the current confirm changes
	$effect(() => {
		if (current) {
			isSubmitted = false;
			isExpanded = false;
		}
	});

	async function handleConfirm(
		confirm: ToolConfirm,
		decision: ToolConfirmDecision,
		toolName?: string
	) {
		if (isSubmitted) return;

		// Only show loading spinner for approve actions, not deny
		if (decision !== 'deny') {
			isSubmitted = true;
		}

		await ChatService.sendToolConfirm(project.assistantID, project.id, currentThreadID, {
			id: confirm.id,
			decision,
			toolName
		});
	}

	function formatJson(jsonString: string): string {
		try {
			const parsed = JSON.parse(jsonString);
			return JSON.stringify(parsed, null, 2);
		} catch {
			return jsonString;
		}
	}
</script>

{#if current}
	{@const dropdown = popover({ placement: 'bottom-end' })}
	<div
		class="bg-surface1 text-on-background mb-2 w-full max-w-[900px] overflow-hidden rounded-xl px-5 shadow-lg"
		transition:slide={{ duration: 150 }}
	>
		{#key current.confirm.id}
			<div class="flex min-h-[48px] items-center gap-3 px-4 py-2.5">
				<!-- Tool name + details toggle -->
				<div class="flex min-w-0 flex-1 items-center gap-2">
					<span class="text-on-background text-sm font-medium">{displayName}</span>
					{#if current.confirm.input}
						<button
							class="text-on-surface1 hover:text-on-background flex items-center gap-1 text-xs"
							onclick={() => (isExpanded = !isExpanded)}
						>
							{#if isExpanded}
								Hide details
							{:else}
								Show details
							{/if}
						</button>
					{/if}
					{#if isSubmitted}
						<LoaderCircle class="text-on-surface1 size-5 animate-spin" />
					{/if}
				</div>

				<!-- Buttons -->
				<div class="flex flex-shrink-0 items-center gap-2">
					{#if !isSubmitted}
						<button
							class="text-on-surface1 hover:bg-surface2 hover:text-on-background rounded px-3 py-1 text-xs transition-colors"
							onclick={() => handleConfirm(current.confirm, 'deny')}
						>
							Deny
						</button>

						<div class="bg-surface2 border-surface2 flex rounded-lg border">
							<button
								class="text-on-background hover:bg-surface3 border-surface3 flex flex-1 items-center justify-center gap-1 rounded-l-lg rounded-r-none border-r px-3 py-1 text-xs transition-colors hover:opacity-80"
								onclick={() => handleConfirm(current.confirm, 'approve')}
							>
								Allow
							</button>

							<button
								use:dropdown.ref
								class="hover:bg-surface3 flex items-center justify-center rounded-l-none rounded-r-lg px-2 py-1 transition-colors hover:opacity-80"
								onclick={() => dropdown.toggle()}
							>
								<ChevronDown class="text-on-background size-3" />
							</button>
						</div>

						<div
							use:dropdown.tooltip
							class="bg-surface2 border-surface3 z-50 flex min-w-[180px] flex-col rounded-lg border py-1 shadow-xl"
						>
							<button
								class="text-on-background hover:bg-surface3 px-3 py-1.5 text-left text-xs transition-colors"
								onclick={() => {
									handleConfirm(current.confirm, 'approve_thread', current.confirm.toolName);
									dropdown.toggle(false);
								}}
							>
								Allow all {current.confirm.toolName} requests
							</button>
							<button
								class="text-on-background hover:bg-surface3 px-3 py-1.5 text-left text-xs transition-colors"
								onclick={() => {
									handleConfirm(current.confirm, 'approve_thread', '*');
									dropdown.toggle(false);
								}}
							>
								Allow all requests
							</button>
						</div>
					{/if}
				</div>
			</div>

			<!-- Expanded input details -->
			{#if isExpanded && current.confirm.input}
				<div class="border-surface2 border-t px-4 py-3" transition:slide={{ duration: 150 }}>
					<pre
						class="bg-background text-on-background max-h-48 overflow-auto rounded p-3 text-xs">{formatJson(
							current.confirm.input
						)}</pre>
				</div>
			{/if}
		{/key}
	</div>
{/if}
