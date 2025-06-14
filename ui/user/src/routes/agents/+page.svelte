<script lang="ts">
	import type { PageProps } from './$types';
	import Navbar from '$lib/components/Navbar.svelte';
	import { darkMode, errors, responsive } from '$lib/stores';
	import { formatTime } from '$lib/time';
	import { getProjectImage } from '$lib/image';
	import { Origami, Plus, Scroll, Server, Trash2 } from 'lucide-svelte';
	import { ChatService, EditorService, type Project } from '$lib/services';
	import Confirm from '$lib/components/Confirm.svelte';
	import { sortByCreatedDate } from '$lib/sort';
	import { clickOutside } from '$lib/actions/clickoutside';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import { goto } from '$app/navigation';
	import AgentCatalog from '$lib/components/agents/AgentCatalog.svelte';
	import { profile } from '$lib/stores';

	import { initHelperMode } from '$lib/context/helperMode.svelte';
	import McpSetupWizard from '$lib/components/mcp/McpSetupWizard.svelte';
	import { initToolReferences } from '$lib/context/toolReferences.svelte';
	import Banner from '$lib/components/Banner.svelte';
	let { data }: PageProps = $props();

	initHelperMode();
	initToolReferences(data.tools);

	let agents = $state(data.projects.filter((p) => p.editor).sort(sortByCreatedDate));
	let chatbots = $state(
		data.projects.filter((p) => !p.editor && !p.sourceProjectID).sort(sortByCreatedDate)
	);

	let toDelete = $state<Project>();
	let createDropdown = $state<HTMLDialogElement>();

	let agentCatalog = $state<ReturnType<typeof AgentCatalog>>();
	let mcpSetupWizard = $state<ReturnType<typeof McpSetupWizard>>();

	async function createNewAgent() {
		try {
			const project = await EditorService.createObot();
			await goto(`/o/${project.id}`);
		} catch (error) {
			errors.append((error as Error).message);
		}
	}
</script>

<div class="flex min-h-dvh flex-col items-center">
	<Navbar />
	<Banner />
	<main
		class="bg-surface1 relative flex w-full grow flex-col items-center justify-center p-4 dark:bg-black"
	>
		{#if agents.length > 0 || chatbots.length > 0}
			<div class="flex w-full max-w-(--breakpoint-xl) grow flex-col gap-6 px-4 py-12">
				{#if agents.length > 0}
					{@render agentsSection()}
				{/if}
				{#if chatbots.length > 0}
					{@render chatbotsSection()}
				{/if}
			</div>
		{:else}
			<div
				class="dark:border-surface3 dark:bg-surface1 w-full max-w-(--breakpoint-xl) rounded-md bg-white px-8 py-12 text-center shadow-sm dark:border"
			>
				<h1 class="mb-2 text-2xl font-bold md:text-3xl">Welcome To Obot!</h1>
				<p class="font-md mb-8 text-gray-500">
					It looks like you haven't created anything just yet. Let's get started!
				</p>

				<div class="grid grid-cols-1 gap-8 md:grid-cols-2 lg:grid-cols-3">
					<button onclick={createNewAgent} class="flex flex-col justify-start gap-2 text-left">
						<img
							src="/agent/images/create-from-scratch.webp"
							alt="Create a template"
							class="aspect-video rounded-md"
						/>
						<p class="flex items-center gap-1 text-base font-semibold">
							<Scroll class="size-4" /> Start From Scratch
						</p>
						<span class="text-sm text-gray-500">
							Start fresh and build exactly what you need.
						</span>
					</button>

					<button
						class="flex flex-col justify-start gap-2 text-left"
						onclick={() => {
							agentCatalog?.open();
						}}
					>
						<img
							src="/agent/images/create-a-template.webp"
							alt="Create a template"
							class="aspect-video rounded-md"
						/>
						<p class="flex items-center gap-1 text-base font-semibold">
							<Origami class="size-4" /> Create From Template
						</p>
						<span class="text-sm text-gray-500">
							Choose a pre-built template to get started quickly.
						</span>
					</button>

					<button
						class="flex flex-col justify-start gap-2 text-left"
						onclick={() => mcpSetupWizard?.open()}
					>
						<img
							src="/agent/images/create-from-mcp.webp"
							alt="Create a template"
							class="aspect-video rounded-md"
						/>
						<p class="flex items-center gap-1 text-base font-semibold">
							<Server class="size-4" /> Browse MCP Catalog
						</p>
						<span class="text-sm text-gray-500"
							>Explore our catalog and set up an agent with an MCP server.</span
						>
					</button>
				</div>
			</div>
		{/if}
	</main>
</div>

{#snippet agentsSection()}
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-semibold">Agents</h1>
		<div class="relative flex items-center gap-4">
			<button
				class="button-primary flex items-center gap-1 text-sm"
				onclick={() => {
					createDropdown?.show();
				}}
			>
				<Plus class="size-6" /> Create New Agent
			</button>

			<dialog
				bind:this={createDropdown}
				class="absolute top-12 right-0 left-auto m-0 w-xs"
				use:clickOutside={() => {
					createDropdown?.close();
				}}
			>
				<div class="flex flex-col gap-2 p-2">
					<button
						class="text-md button hover:bg-surface1 dark:hover:bg-surface3 flex w-full items-center gap-2 rounded-sm bg-transparent px-2 font-light"
						onclick={createNewAgent}
					>
						<Scroll class="size-4" /> Create From Scratch
					</button>
					<button
						class="text-md button hover:bg-surface1 dark:hover:bg-surface3 flex w-full items-center gap-2 rounded-sm bg-transparent px-2 font-light"
						onclick={() => {
							agentCatalog?.open();
							createDropdown?.close();
						}}
					>
						<Origami class="size-4" /> Create From Template
					</button>
					<button
						class="text-md button hover:bg-surface1 dark:hover:bg-surface3 flex w-full items-center gap-2 rounded-sm bg-transparent px-2 font-light"
						onclick={() => {
							mcpSetupWizard?.open();
							createDropdown?.close();
						}}
					>
						<Server class="size-4" /> Create From MCP Catalog
					</button>
				</div>
			</dialog>
		</div>
	</div>
	{@render table(agents)}
{/snippet}

{#snippet chatbotsSection()}
	<h1 class="text-2xl font-semibold">Chatbots</h1>
	{@render table(chatbots, true)}
{/snippet}

{#snippet table(projects: Project[], displayOwner?: boolean)}
	<div class="dark:bg-surface2 w-full overflow-hidden rounded-md bg-white shadow-sm">
		<table class="w-full border-collapse">
			<thead class="dark:bg-surface1 bg-surface2">
				<tr>
					<th class="text-md w-1/2 px-4 py-2 text-left font-medium text-gray-500">Name</th>
					{#if !responsive.isMobile}
						{#if displayOwner}
							<th class="text-md w-1/4 px-4 py-2 text-left font-medium text-gray-500">Owner</th>
						{/if}
						<th class="text-md w-1/4 px-4 py-2 text-left font-medium text-gray-500">Created</th>
					{/if}
					<th class="text-md float-right w-auto px-4 py-2 text-left font-medium text-gray-500"
						>Actions</th
					>
				</tr>
			</thead>
			<tbody>
				{#each projects as project (project.id)}
					{@render row(project)}
				{/each}
			</tbody>
		</table>
	</div>
{/snippet}

{#snippet row(project: Project, displayOwner?: boolean)}
	<tr class="border-surface2 dark:border-surface2 border-t shadow-xs">
		<td>
			<a href={`/o/${project.id}`}>
				<div class="flex h-full w-full items-center gap-2 px-4 py-2">
					<div
						class="bg-surface1 flex size-10 flex-shrink-0 items-center rounded-sm p-1 shadow-sm dark:bg-gray-600"
					>
						<img src={getProjectImage(project, darkMode.isDark)} alt={project.name} />
					</div>
					<div class="flex flex-col">
						<h4 class="line-clamp-1 text-sm font-medium" class:text-gray-500={!project.name}>
							{project.name || 'Untitled'}
						</h4>
						<p class="line-clamp-1 text-xs font-light" class:text-gray-300={!project.description}>
							{project.description || 'No description'}
						</p>
					</div>
				</div>
			</a>
		</td>
		{#if !responsive.isMobile}
			{#if displayOwner}
				<td class="text-sm font-light">
					<a class="flex h-full w-full px-4 py-2" href={`/o/${project.id}`}>Unspecified</a>
				</td>
			{/if}
			<td class="text-sm font-light">
				<a class="flex h-full w-full px-4 py-2" href={`/o/${project.id}`}
					>{formatTime(project.created)}</a
				>
			</td>
		{/if}
		{#if project.userID === profile.current.id}
			<td class="flex justify-end px-4 py-2 text-sm font-light">
				<button
					class="icon-button"
					onclick={() => (toDelete = project)}
					use:tooltip={'Delete agent'}
				>
					<Trash2 class="size-4" />
				</button>
			</td>
		{/if}
	</tr>
{/snippet}

<Confirm
	msg={toDelete?.editor
		? `Delete agent: ${toDelete?.name || 'Untitled'}?`
		: `Remove agent: ${toDelete?.name || 'Untitled'}?`}
	show={!!toDelete}
	onsuccess={async () => {
		if (!toDelete) return;
		try {
			await ChatService.deleteProject(toDelete.assistantID, toDelete.id);

			if (toDelete.editor) {
				agents = agents.filter((p) => p.id !== toDelete?.id);
			} else {
				chatbots = chatbots.filter((p) => p.id !== toDelete?.id);
			}
		} finally {
			toDelete = undefined;
		}
	}}
	oncancel={() => (toDelete = undefined)}
/>

<AgentCatalog bind:this={agentCatalog} />

<McpSetupWizard
	bind:this={mcpSetupWizard}
	catalogDescription="Extend your agent's capabilities by adding multiple MCP servers from our evergrowing catalog."
	catalogSubmitText="Create agent with server"
/>

<svelte:head>
	<title>Obot | Agents</title>
</svelte:head>
