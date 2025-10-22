<script lang="ts">
	import ResponsiveDialog from '../ResponsiveDialog.svelte';
	import { AlertCircle, LoaderCircle, Server } from 'lucide-svelte';
	import InfoTooltip from '../InfoTooltip.svelte';
	import SensitiveInput from '../SensitiveInput.svelte';
	import { twMerge } from 'tailwind-merge';
	import type { Snippet } from 'svelte';

	// Component manifest types are loosely typed here to avoid tight coupling
	type SubField = {
		key: string;
		name: string;
		description?: string;
		required?: boolean;
		sensitive?: boolean;
		file?: boolean;
	};

	type ComponentManifest = {
		name?: string;
		icon?: string;
		description?: string;
		env?: SubField[];
		remoteConfig?: {
			fixedURL?: string;
			hostname?: string;
			urlTemplate?: string;
			headers?: SubField[];
		};
	};

	export type ComponentConfigRequest = Record<
		string,
		{ config: Record<string, string>; url?: string; skip?: boolean }
	>;

	interface Props {
		name?: string;
		icon?: string;
		components: { catalogEntryID: string; manifest: ComponentManifest }[];
		initialValues?: ComponentConfigRequest;
		onSave?: (configs: ComponentConfigRequest) => void;
		onCancel?: () => void;
		onClose?: () => void;
		loading?: boolean;
		error?: string;
		submitText?: string;
		cancelText?: string;
		actions?: Snippet;
	}

	let {
		name,
		icon,
		components = [],
		initialValues,
		onSave,
		onCancel,
		onClose,
		loading,
		error,
		submitText = 'Save',
		cancelText = 'Cancel'
	}: Props = $props();

	let dialog = $state<ReturnType<typeof ResponsiveDialog>>();
	let isOpen = $state(false);

	// Track per-component field values
	let valuesById = $state<ComponentConfigRequest>({});
	let highlightById = $state<Record<string, Set<string>>>({});

	function initValues() {
		const map: ComponentConfigRequest = {};
		for (const c of components) {
			const existing = initialValues?.[c.catalogEntryID];
			map[c.catalogEntryID] = {
				config: { ...(existing?.config || {}) },
				url: existing?.url ?? c.manifest.remoteConfig?.fixedURL ?? '',
				skip: existing?.skip ?? false
			};
		}
		valuesById = map;
		highlightById = {};
	}

	export function open() {
		initValues();
		dialog?.open();
		isOpen = true;
	}

	export function close() {
		dialog?.close();
		isOpen = false;
	}

	function needsUrlInput(manifest?: ComponentManifest): boolean {
		const rc = manifest?.remoteConfig;
		if (!rc) return false;
		if (rc.fixedURL) return false;
		if (rc.urlTemplate) return false; // template URL computed on backend
		return Boolean(rc.hostname);
	}

	function validate(): boolean {
		let ok = true;
		const newHighlights: Record<string, Set<string>> = {};
		for (const c of components) {
			const id = c.catalogEntryID;
			const manifest = c.manifest;
			const val = valuesById[id] ?? { config: {}, url: '' };
			const set = new Set<string>();

			for (const env of manifest.env || []) {
				if (env.required && !val.config[env.key]) set.add(env.key);
			}
			for (const h of manifest.remoteConfig?.headers || []) {
				if (h.required && !val.config[h.key]) set.add(h.key);
			}
			if (needsUrlInput(manifest) && !(val.url && val.url.trim())) {
				set.add('url');
			}
			if (set.size > 0 && !val.skip) ok = false;
			if (set.size > 0) newHighlights[id] = set;
		}
		highlightById = newHighlights;
		return ok;
	}

	function handleSave() {
		if (!validate()) return;
		onSave?.(valuesById);
	}
</script>

<ResponsiveDialog
	bind:this={dialog}
	onClose={() => {
		onClose?.();
		isOpen = false;
	}}
>
	{#snippet titleContent()}
		<div class="flex items-center gap-2">
			<div class="bg-surface1 rounded-sm p-1 dark:bg-gray-600">
				{#if icon}
					<img src={icon} alt={name} class="size-8" />
				{:else}
					<Server class="size-8" />
				{/if}
			</div>
			{name}
		</div>
	{/snippet}

	{#if isOpen}
		{#if error}
			<div class="notification-error flex items-center gap-2">
				<AlertCircle class="size-6 flex-shrink-0 text-red-500" />
				<p class="flex flex-col text-sm font-light">
					<span class="font-semibold">Error:</span>
					<span>{error}</span>
				</p>
			</div>
		{/if}

		<div class="my-4 flex flex-col gap-4">
			{#each components as c (c.catalogEntryID)}
				{@const manifest = c.manifest}
				{@const id = c.catalogEntryID}
				<div class="dark:bg-surface2 dark:border-surface3 rounded-lg border border-gray-200">
					<div class="flex items-center gap-3 p-3">
						<div class="bg-surface1 rounded-sm p-1 dark:bg-gray-600">
							{#if manifest.icon}
								<img src={manifest.icon} alt={manifest.name} class="size-8" />
							{:else}
								<Server class="size-8" />
							{/if}
						</div>
						<div class="flex flex-col">
							<div class="font-medium">{manifest.name}</div>
							{#if manifest.description}
								<div class="text-sm text-gray-500 dark:text-gray-400">{manifest.description}</div>
							{/if}
						</div>
						<label class="ml-auto flex items-center gap-2 text-sm">
							<input type="checkbox" bind:checked={valuesById[id].skip} /> Skip
						</label>
					</div>
					<div class="border-t border-gray-200 p-3">
						{#if manifest.env && manifest.env.length > 0}
							<div class="mb-2 text-xs font-semibold">Environment</div>
							{#each manifest.env as env (env.key)}
								{@const hl = highlightById[id]?.has(env.key)}
								<div class="mb-3 flex flex-col gap-1">
									<span class="flex items-center gap-2">
										<label for={`${id}-${env.key}`} class={hl ? 'text-red-500' : ''}>
											{env.name}
											{#if !env.required}
												<span class="text-gray-400 dark:text-gray-600">(optional)</span>
											{/if}
										</label>
										<InfoTooltip text={env.description || ''} />
									</span>
									{#if env.sensitive}
										<SensitiveInput
											name={env.name}
											bind:value={valuesById[id].config[env.key]}
											textarea={env.file}
											growable
											error={Boolean(hl)}
										/>
									{:else if env.file}
										<textarea
											id={`${id}-${env.key}`}
											bind:value={valuesById[id].config[env.key]}
											class={twMerge(
												'text-input-filled h-32 resize-y whitespace-pre-wrap',
												hl && 'border-red-500 bg-red-500/20 ring-red-500 focus:ring-1'
											)}
										></textarea>
									{:else}
										<input
											type="text"
											id={`${id}-${env.key}`}
											bind:value={valuesById[id].config[env.key]}
											class={twMerge(
												'text-input-filled',
												hl && 'border-red-500 bg-red-500/20 ring-red-500 focus:ring-1'
											)}
										/>
									{/if}
								</div>
							{/each}
						{/if}

						{#if manifest.remoteConfig?.headers && manifest.remoteConfig.headers.length > 0}
							<div class="mb-2 text-xs font-semibold">Headers</div>
							{#each manifest.remoteConfig.headers as header (header.key)}
								{#if header.required}
									{@const hl = highlightById[id]?.has(header.key)}
									<div class="mb-3 flex flex-col gap-1">
										<span class="flex items-center gap-2">
											<label for={`${id}-${header.key}`} class={hl ? 'text-red-500' : ''}>
												{header.name}
											</label>
											<InfoTooltip text={header.description || ''} />
										</span>
										{#if header.sensitive}
											<SensitiveInput
												name={header.name}
												bind:value={valuesById[id].config[header.key]}
												error={Boolean(hl)}
											/>
										{:else}
											<input
												type="text"
												id={`${id}-${header.key}`}
												bind:value={valuesById[id].config[header.key]}
												class={twMerge(
													'text-input-filled',
													hl && 'border-red-500 bg-red-500/20 ring-red-500 focus:ring-1'
												)}
											/>
										{/if}
									</div>
								{/if}
							{/each}
						{/if}

						{#if needsUrlInput(manifest)}
							{@const hl = highlightById[id]?.has('url')}
							<label for={`${id}-url`}> URL </label>
							<input
								type="text"
								id={`${id}-url`}
								bind:value={valuesById[id].url}
								class={twMerge(
									'text-input-filled',
									hl && 'border-red-500 bg-red-500/20 ring-red-500 focus:ring-1'
								)}
							/>
							{#if manifest.remoteConfig?.hostname}
								<span class="font-light text-gray-400 dark:text-gray-600">
									The URL must contain the hostname: <b class="font-semibold"
										>{manifest.remoteConfig.hostname}</b
									>
								</span>
							{/if}
						{:else if manifest.remoteConfig?.fixedURL}
							<label for={`${id}-fixed-url`}> URL </label>
							<input
								id={`${id}-fixed-url`}
								type="text"
								class="text-input-filled"
								value={manifest.remoteConfig.fixedURL}
								disabled
							/>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}

	<div class="flex justify-end gap-2">
		{#if onCancel}
			<button class="button" onclick={onCancel} disabled={loading}>{cancelText}</button>
		{/if}
		<button class="button-primary" onclick={handleSave} disabled={loading}>
			{#if loading}
				<LoaderCircle class="size-4 animate-spin" />
			{:else}
				{submitText}
			{/if}
		</button>
	</div>
</ResponsiveDialog>
