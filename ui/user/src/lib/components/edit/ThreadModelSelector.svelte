<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { ChevronDown } from 'lucide-svelte';
	import type { ModelProvider, Thread as ThreadType } from '$lib/services/chat/types';
	import type { Project } from '$lib/services';
	import {
		getThread,
		updateThread,
		getDefaultModelForThread,
		listModelProviders
	} from '$lib/services/chat/operations';
	import { twMerge } from 'tailwind-merge';
	import { SvelteMap } from 'svelte/reactivity';
	import { darkMode } from '$lib/stores';

	interface Props {
		threadId: string | undefined;
		project: Project;
		onModelChanged?: () => void;
	}

	let { threadId, project, onModelChanged }: Props = $props();

	let showModelSelector = $state(false);
	let threadDetails = $state<ThreadType | null>(null);
	let isUpdatingModel = $state(false);
	let modelSelectorRef = $state<HTMLDivElement>();
	let modelButtonRef = $state<HTMLButtonElement>();
	let defaultModel = $state<{ model: string; modelProvider: string } | null>(null);

	let modelsEntries = $derived(Object.entries(project.models || {}));

	const isDefaultModelSelected = $derived(
		threadDetails && defaultModel?.model !== '' && defaultModel?.model === threadDetails?.model
	);

	// Function to fetch thread details including model
	async function fetchThreadDetails() {
		if (!threadId) return;

		try {
			const thread = await getThread(project.assistantID, project.id, threadId);
			threadDetails = thread;

			// Fetch default model information
			fetchDefaultModel();
		} catch (err) {
			console.error('Error fetching thread details:', err);
		}
	}

	// Function to fetch default model for this thread
	async function fetchDefaultModel() {
		if (!threadId) return;

		try {
			defaultModel = await getDefaultModelForThread(project.assistantID, project.id, threadId);
		} catch (err) {
			console.error('Error fetching default model:', err);
			defaultModel = null;
		}
	}

	// Function to update thread model
	async function setThreadModel(model: string, provider: string) {
		if (!threadId || !threadDetails) return;

		// Prevent setting to empty if default model is empty
		if (!model && !provider && defaultModel?.model === '' && defaultModel?.modelProvider === '') {
			return;
		}

		isUpdatingModel = true;

		try {
			const updatedThread = await updateThread(project.assistantID, project.id, {
				...threadDetails,
				model: model || undefined,
				modelProvider: provider || undefined
			});

			// Update local state
			threadDetails = updatedThread;

			// If resetting to default, fetch the default model
			if (!model && !provider) {
				fetchDefaultModel();
			}

			// Close dropdown
			showModelSelector = false;

			// Notify parent that model changed
			if (onModelChanged) {
				onModelChanged();
			}
		} catch (err) {
			console.error('Error updating thread model:', err);
		} finally {
			isUpdatingModel = false;
		}
	}

	onMount(() => {
		// Close model selector when clicking outside
		const handleClickOutside = (event: MouseEvent) => {
			if (
				showModelSelector &&
				modelSelectorRef &&
				modelButtonRef &&
				!modelSelectorRef.contains(event.target as Node) &&
				!modelButtonRef.contains(event.target as Node)
			) {
				showModelSelector = false;
			}
		};

		window.addEventListener('click', handleClickOutside);

		return () => {
			window.removeEventListener('click', handleClickOutside);
		};
	});

	$effect(() => {
		if (threadId) {
			fetchThreadDetails().then(() => {
				if (threadDetails && threadDetails.model && threadDetails.modelProvider) {
					// Make sure that the thread model is available on the project, and replace it with default if not.
					if (
						!project.models ||
						!project.models[threadDetails.modelProvider] ||
						!project.models[threadDetails.modelProvider].includes(threadDetails.model)
					) {
						setThreadModel('', '');
					}
				}
			});
		}
	});

	let modelProvidersMap = new SvelteMap<string, ModelProvider>();

	$effect(() => {
		loadModelProviders(project);
	});

	// Function to fetch model providers
	async function loadModelProviders(project: Project) {
		try {
			listModelProviders(project.assistantID, project.id).then((res) => {
				untrack(() => {
					for (const provider of res.items) {
						modelProvidersMap.set(provider.id, provider);
					}
				});
			});
		} catch (error) {
			console.error('Failed to load model providers:', error);
		}
	}

	type ScrollIntoSelectedModelParams = {
		providerId?: string;
		modelId?: string;
	};

	// TODO: We are loading model providers in different location in the app
	// A better approach to load them once and share them, with the abbility to reload the results
	function scrollIntoSelectedModel(node: HTMLElement, params: ScrollIntoSelectedModelParams) {
		if (!params.modelId) return;
		if (!params.providerId) return;

		tick().then(() => {
			const modelElement = node.querySelector(
				`[data-provider="${params.providerId}"][data-model="${params.modelId}"]`
			);
			if (modelElement) {
				modelElement.scrollIntoView({ behavior: 'instant', block: 'center' });
			}
		});
	}
</script>

<!-- TODO: Refactor this to use a dropdown component either third-party or internally crafted -->
<div class="relative mr-2 md:mr-6 lg:mr-8">
	<button
		class={twMerge(
			'hover:bg-surface2/50 active:bg-surface2/80 flex h-10 items-center gap-3 rounded-full px-2  py-1 text-xs text-gray-600 md:px-4 lg:px-6',
			(isDefaultModelSelected || (!threadDetails?.model && defaultModel?.model)) &&
				'text-blue hover:bg-blue/10 active:bg-blue/15 bg-transparent'
		)}
		onclick={(e) => {
			e.stopPropagation();
			showModelSelector = !showModelSelector;
		}}
		onkeydown={(e) => e.key === 'Escape' && (showModelSelector = false)}
		aria-haspopup="listbox"
		aria-expanded={showModelSelector}
		id="thread-model-button"
		title={isDefaultModelSelected
			? 'Default model is selected'
			: threadDetails?.model
				? ''
				: 'Select model for this thread'}
		bind:this={modelButtonRef}
	>
		<div class="max-w-40 truncate sm:max-w-60 md:max-w-96 lg:max-w-none">
			{#if threadDetails?.modelProvider && threadDetails?.model}
				{threadDetails.model}
			{:else if defaultModel?.model && defaultModel.model !== ''}
				{defaultModel.model}
			{:else}
				No Default Model
			{/if}
		</div>

		<ChevronDown class="h-4 w-4" />
	</button>

	{#if showModelSelector}
		<div
			role="listbox"
			tabindex="-1"
			aria-labelledby="thread-model-button"
			class="available-models-popover default-scrollbar-thin border-surface1 dark:bg-surface2 absolute right-0 bottom-full z-10 mb-1 max-h-60 w-max max-w-sm overflow-hidden overflow-y-auto rounded-md border bg-white px-2 shadow-lg md:max-w-md lg:max-w-lg"
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => {
				if (e.key === 'Escape') {
					showModelSelector = false;
					document.getElementById('thread-model-button')?.focus();
				}
			}}
			bind:this={modelSelectorRef}
			use:scrollIntoSelectedModel={{
				providerId: threadDetails?.modelProvider ?? defaultModel?.modelProvider,
				modelId: threadDetails?.model ?? defaultModel?.model
			}}
		>
			{#if modelsEntries.length}
				<div class="flex flex-col">
					{#each modelsEntries as [providerId, models] (providerId)}
						{#if Array.isArray(models) && models.length > 0 && providerId}
							{@const provider = modelProvidersMap.get(providerId)}
							<div class="border-surface1 flex flex-col border-b py-2 last:border-transparent">
								<div class="mb-2 flex gap-1 text-xs">
									{#if provider?.icon || provider?.iconDark}
										<img
											src={darkMode.isDark && provider.iconDark ? provider.iconDark : provider.icon}
											alt={provider.name}
											class={twMerge(
												'size-4',
												darkMode.isDark && !provider.iconDark ? 'dark:invert' : ''
											)}
										/>
									{/if}
									<div>{provider?.name ?? ''}</div>
								</div>
								<div class="provider-models flex flex-col gap-1">
									{#each models as model (model)}
										{@const isModelSelected =
											threadDetails?.modelProvider === providerId && threadDetails?.model === model}

										{@const isDefaultModel =
											defaultModel?.modelProvider === providerId && defaultModel?.model === model}

										<button
											role="option"
											aria-selected={isModelSelected}
											class={twMerge(
												'hover:bg-surface1/70 active:bg-surface1/80 focus:bg-surface1/70 flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors duration-200 focus:outline-none',
												isModelSelected && 'text-blue bg-blue/10 hover:bg-blue/15 active:bg-blue/20'
											)}
											onclick={() => setThreadModel(model, providerId)}
											tabindex="0"
											data-provider={providerId}
											data-model={model}
										>
											<div>
												{model}
											</div>

											{#if isDefaultModel}
												<img
													class={twMerge(' size-4', !isModelSelected && 'grayscale-100')}
													src="/user/images/obot-icon-blue.svg"
													alt="Obot default model"
													title="Obot default model"
												/>
											{/if}

											{#if threadDetails?.modelProvider === providerId && threadDetails?.model === model}
												<div class="ml-auto text-xs text-blue-500">✓</div>
											{/if}
										</button>
									{/each}
								</div>
							</div>
						{/if}
					{/each}
				</div>

				{#if isUpdatingModel}
					<div class="flex justify-center p-2">
						<div
							class="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"
							aria-hidden="true"
						></div>
						<span class="sr-only">Loading...</span>
					</div>
				{/if}
			{:else}
				<p class="truncate text-sm text-gray-400">See "Configuration" for more options</p>
			{/if}
		</div>
	{/if}
</div>

<style>
	.available-models-popover {
		display: grid;
		grid-template-columns: minmax(fit-content, auto);
	}
</style>
