<script lang="ts">
	import type { MCPServerInfo } from '$lib/services/chat/mcp';
	import { Plus, Trash2 } from 'lucide-svelte';
	import InfoTooltip from '$lib/components/InfoTooltip.svelte';
	import SensitiveInput from '$lib/components/SensitiveInput.svelte';
	import { onMount } from 'svelte';

	interface Props {
		config: MCPServerInfo;
		showSubmitError: boolean;
		custom?: boolean;
	}
	let { config = $bindable(), showSubmitError, custom }: Props = $props();

	function focusOnAdd(node: HTMLInputElement, shouldFocus: boolean) {
		if (shouldFocus) {
			node.focus();
		}
	}

	// Function to extract variables from arguments containing ${VAR} patterns
	function extractAndAddEnvVars() {
		if (!config.env) return;

		// Regular expression to match ${VARIABLE_NAME}
		const varRegex = /\${([A-Za-z0-9_]+)}/g;
		const varsToAdd = new Set<string>();

		// Check command for variables
		if (config.command) {
			let match;
			const commandCopy = config.command;
			while ((match = varRegex.exec(commandCopy)) !== null) {
				varsToAdd.add(match[1]);
			}
		}

		// Check each argument for variables
		if (config.args) {
			config.args.forEach((arg) => {
				let match;
				const argCopy = arg;
				while ((match = varRegex.exec(argCopy)) !== null) {
					varsToAdd.add(match[1]);
				}
			});
		}

		// Add missing variables as required environment variables
		varsToAdd.forEach((varName) => {
			// Check if the variable already exists in the environment
			const exists = config.env?.some((env) => env.key === varName);
			if (!exists) {
				config.env?.push({
					name: varName,
					key: varName,
					description: `Required for command/argument with \${${varName}}`,
					sensitive: true,
					required: true,
					file: false,
					value: ''
				});
			}
		});
	}

	// Watch for changes to arguments and command, then extract variables
	$effect(() => {
		extractAndAddEnvVars();
	});

	// Run extraction on component mount for existing values
	onMount(() => {
		extractAndAddEnvVars();
	});
</script>

{#if config.env}
	<div class="flex flex-col gap-1">
		<h4 class="text-base font-semibold">Environment Variables</h4>
		{#each config.env as env, i}
			<div class="flex w-full items-center gap-2">
				<div class="flex grow flex-col gap-1">
					{#if !env.required}
						<input
							class="ghost-input w-full py-0"
							bind:value={env.key}
							placeholder="Key (ex. API_KEY)"
							use:focusOnAdd={i === config.env.length - 1}
						/>
					{:else}
						<label for={env.name} class="flex items-center gap-1 text-sm font-light">
							{env.required ? `${env.name ?? env.key}*` : `${env.name ?? env.key} (optional)`}
							<InfoTooltip text={env.description} />
						</label>
					{/if}
					{#if env.sensitive}
						<SensitiveInput name={env.name} bind:value={env.value} />
					{:else}
						<input
							data-1p-ignore
							id={env.name}
							name={env.name}
							class="text-input-filled w-full"
							class:error={showSubmitError && !env.value && env.required}
							bind:value={env.value}
							type="text"
						/>
					{/if}

					<div class="min-h-4 text-xs text-red-500">
						{#if showSubmitError && !env.value && env.required}
							This field is required.
						{/if}
					</div>
				</div>
				{#if !env.required || custom}
					<button class="icon-button" onclick={() => config.env?.splice(i, 1)}>
						<Trash2 class="size-4" />
					</button>
				{/if}
			</div>
		{/each}
		<div class="flex justify-end">
			<button
				class="button flex items-center gap-1 text-xs"
				onclick={() =>
					config.env?.push({
						name: '',
						key: '',
						description: '',
						sensitive: false,
						required: false,
						file: false,
						value: ''
					})}
			>
				<Plus class="size-4" /> Environment Variable
			</button>
		</div>
	</div>
{/if}

<div class="flex items-center gap-4">
	<h4 class="text-base font-semibold">Command</h4>
	<input
		class="text-input-filled w-full"
		bind:value={config.command}
		oninput={extractAndAddEnvVars}
	/>
</div>

{#if config.args}
	<div class="flex gap-4">
		<h4 class="mt-1.5 text-base font-semibold">Arguments</h4>
		<div class="flex grow flex-col gap-4">
			{#each config.args as _arg, i}
				<div class="flex items-center gap-2">
					<input
						class="text-input-filled w-full"
						bind:value={config.args[i]}
						oninput={extractAndAddEnvVars}
					/>
					<button class="icon-button" onclick={() => config.args?.splice(i, 1)}>
						<Trash2 class="size-4" />
					</button>
				</div>
			{/each}

			<div class="flex justify-end">
				<button
					class="button flex items-center gap-1 text-xs"
					onclick={() => {
						config.args?.push('');
						setTimeout(extractAndAddEnvVars, 0);
					}}
				>
					<Plus class="size-4" /> Argument
				</button>
			</div>
		</div>
	</div>
{/if}
