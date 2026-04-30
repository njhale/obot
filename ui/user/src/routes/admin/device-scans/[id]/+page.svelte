<script lang="ts">
	import { page } from '$app/state';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import CopyButton from '$lib/components/CopyButton.svelte';
	import Layout from '$lib/components/Layout.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import { PAGE_TRANSITION_DURATION } from '$lib/constants';
	import type {
		DeviceScan,
		DeviceScanFile,
		DeviceScanMCPServer,
		DeviceScanPlugin,
		DeviceScanSkill
	} from '$lib/services/admin/types';
	import { formatTimeAgo } from '$lib/time';
	import { goto } from '$lib/url';
	import { openUrl } from '$lib/utils';
	import { Cpu, FileText, PencilRuler, Server, Boxes } from 'lucide-svelte';
	import { fly } from 'svelte/transition';

	type Tab = 'mcp' | 'skills' | 'plugins' | 'files';

	let { data } = $props();
	let scan = $derived<DeviceScan | undefined>(data?.scan);
	let activeTab = $state<Tab>('mcp');

	const duration = PAGE_TRANSITION_DURATION;

	let mcpServers = $derived<DeviceScanMCPServer[]>(scan?.mcp_servers ?? []);
	let skills = $derived<DeviceScanSkill[]>(scan?.skills ?? []);
	let plugins = $derived<DeviceScanPlugin[]>(scan?.plugins ?? []);
	let files = $derived<DeviceScanFile[]>(scan?.files ?? []);

	let scannedTime = $derived(
		scan ? formatTimeAgo(scan.scanned_at) : { relativeTime: '', fullDate: '' }
	);

	type MCPRow = DeviceScanMCPServer & { id: string; index: number; endpoint: string };
	type SkillRow = DeviceScanSkill & { id: string; index: number };
	type PluginRow = DeviceScanPlugin & { id: string; index: number; capabilities: string };
	type FileRow = DeviceScanFile & { id: string; size_display: string };

	let mcpRows = $derived<MCPRow[]>(
		mcpServers.map((m, i) => ({
			...m,
			id: `${m.client}-${m.scope}-${m.name}-${i}`,
			index: i,
			endpoint: m.transport === 'stdio' ? formatCommand(m.command, m.args) : m.url || '—'
		}))
	);

	let skillRows = $derived<SkillRow[]>(
		skills.map((s, i) => ({ ...s, id: `${s.client}-${s.scope}-${s.name}-${i}`, index: i }))
	);

	let pluginRows = $derived<PluginRow[]>(
		plugins.map((p, i) => ({
			...p,
			id: `${p.client}-${p.scope}-${p.name}-${i}`,
			index: i,
			capabilities: capabilitySummary(p)
		}))
	);

	let fileRows = $derived<FileRow[]>(
		files.map((f, i) => ({ ...f, id: `${f.path}-${i}`, size_display: formatBytes(f.size_bytes) }))
	);

	let scanId = $derived(page.params.id);

	function formatCommand(cmd?: string, args?: string[]): string {
		if (!cmd) return '—';
		const parts = [cmd, ...(args ?? [])];
		return parts.join(' ');
	}

	function capabilitySummary(p: DeviceScanPlugin): string {
		const caps: string[] = [];
		if (p.has_mcp_servers) caps.push('mcp');
		if (p.has_skills) caps.push('skills');
		if (p.has_rules) caps.push('rules');
		if (p.has_commands) caps.push('commands');
		if (p.has_hooks) caps.push('hooks');
		return caps.length ? caps.join(', ') : '—';
	}

	function formatBytes(n: number): string {
		if (n < 1024) return `${n} B`;
		if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
		return `${(n / 1024 / 1024).toFixed(1)} MiB`;
	}
</script>

<svelte:head>
	<title>Obot | Device Scan</title>
</svelte:head>

<Layout title="Device Scan" showBackButton onBackButtonClick={() => goto('/admin/device-scans')}>
	<div
		class="flex flex-col gap-6"
		in:fly={{ x: 100, duration, delay: duration }}
		out:fly={{ x: -100, duration }}
	>
		{#if !scan}
			<p class="text-on-surface1 text-sm font-light">Scan not found.</p>
		{:else}
			<!-- Header card -->
			<div
				class="dark:bg-surface2 bg-background flex flex-col gap-4 rounded-md p-4 shadow-sm md:flex-row md:items-start md:justify-between"
			>
				<div class="flex flex-col gap-2">
					<h2 class="flex items-center gap-2 font-mono text-xl font-semibold">
						{scan.device_id}
						<CopyButton text={scan.device_id} />
					</h2>
					<div class="flex flex-wrap items-center gap-3 text-sm">
						<span class="pill-primary bg-primary">{scan.os}/{scan.arch}</span>
						{#if scan.username}
							<span class="text-on-surface1">
								user <span class="font-mono">{scan.username}</span>
							</span>
						{/if}
						<span class="text-on-surface1">
							scanner <span class="font-mono">{scan.scanner_version || '—'}</span>
						</span>
						<span class="text-on-surface1" use:tooltip={scannedTime.fullDate}>
							scanned {scannedTime.relativeTime || '—'}
						</span>
					</div>
					{#if scan.submitted_by}
						<div class="flex items-center gap-1 text-xs">
							<span class="text-on-surface1">submitted by</span>
							<span class="font-mono">{scan.submitted_by}</span>
						</div>
					{/if}
				</div>
			</div>

			<!-- Tabs -->
			<div class="flex flex-col gap-2">
				<div class="border-surface2 dark:border-surface2 flex gap-2 border-b">
					<button
						class="tab-button"
						class:tab-active={activeTab === 'mcp'}
						onclick={() => (activeTab = 'mcp')}
					>
						<Server class="size-4" /> MCP Servers
						<span class="text-on-surface1">({mcpServers.length})</span>
					</button>
					<button
						class="tab-button"
						class:tab-active={activeTab === 'skills'}
						onclick={() => (activeTab = 'skills')}
					>
						<PencilRuler class="size-4" /> Skills
						<span class="text-on-surface1">({skills.length})</span>
					</button>
					<button
						class="tab-button"
						class:tab-active={activeTab === 'plugins'}
						onclick={() => (activeTab = 'plugins')}
					>
						<Boxes class="size-4" /> Plugins
						<span class="text-on-surface1">({plugins.length})</span>
					</button>
					<button
						class="tab-button"
						class:tab-active={activeTab === 'files'}
						onclick={() => (activeTab = 'files')}
					>
						<FileText class="size-4" /> Files <span class="text-on-surface1">({files.length})</span>
					</button>
				</div>

				{#if activeTab === 'mcp'}
					{#if mcpRows.length === 0}
						{@render emptyTab('No MCP servers found in this scan.')}
					{:else}
						<Table
							data={mcpRows}
							fields={['client', 'scope', 'name', 'transport', 'endpoint']}
							headers={[
								{ title: 'Client', property: 'client' },
								{ title: 'Scope', property: 'scope' },
								{ title: 'Name', property: 'name' },
								{ title: 'Transport', property: 'transport' },
								{ title: 'Endpoint', property: 'endpoint' }
							]}
							sortable={['client', 'name', 'transport', 'scope']}
							filterable={['client', 'transport', 'scope']}
							onClickRow={(d, isCtrlClick) => {
								openUrl(`/admin/device-scans/${scanId}/mcp/${d.index}`, isCtrlClick);
							}}
						>
							{#snippet onRenderColumn(property, d: MCPRow)}
								{#if property === 'name'}
									<span class="font-mono text-xs">{d.name}</span>
								{:else if property === 'endpoint'}
									<span class="font-mono text-xs">{d.endpoint}</span>
								{:else}
									{d[property as keyof MCPRow] ?? '—'}
								{/if}
							{/snippet}
						</Table>
					{/if}
				{:else if activeTab === 'skills'}
					{#if skillRows.length === 0}
						{@render emptyTab('No skills found in this scan.')}
					{:else}
						<Table
							data={skillRows}
							fields={['client', 'scope', 'name', 'description', 'has_scripts', 'files_count']}
							headers={[
								{ title: 'Client', property: 'client' },
								{ title: 'Scope', property: 'scope' },
								{ title: 'Name', property: 'name' },
								{ title: 'Description', property: 'description' },
								{ title: 'Has Scripts', property: 'has_scripts' },
								{ title: 'Files', property: 'files_count' }
							]}
							sortable={['client', 'name', 'scope']}
							filterable={['client', 'scope']}
							onClickRow={(d, isCtrlClick) => {
								openUrl(`/admin/device-scans/${scanId}/skills/${d.index}`, isCtrlClick);
							}}
						>
							{#snippet onRenderColumn(property, d: SkillRow)}
								{#if property === 'description'}
									<span class="text-on-surface1 text-xs">{d.description ?? '—'}</span>
								{:else if property === 'has_scripts'}
									{d.has_scripts ? 'yes' : 'no'}
								{:else if property === 'files_count'}
									{(d.files ?? []).length}
								{:else}
									{d[property as keyof SkillRow] ?? '—'}
								{/if}
							{/snippet}
						</Table>
					{/if}
				{:else if activeTab === 'plugins'}
					{#if pluginRows.length === 0}
						{@render emptyTab('No plugins found in this scan.')}
					{:else}
						<Table
							data={pluginRows}
							fields={[
								'client',
								'scope',
								'name',
								'plugin_type',
								'version',
								'enabled',
								'capabilities'
							]}
							headers={[
								{ title: 'Client', property: 'client' },
								{ title: 'Scope', property: 'scope' },
								{ title: 'Name', property: 'name' },
								{ title: 'Type', property: 'plugin_type' },
								{ title: 'Version', property: 'version' },
								{ title: 'Enabled', property: 'enabled' },
								{ title: 'Capabilities', property: 'capabilities' }
							]}
							sortable={['client', 'name', 'plugin_type', 'version']}
							filterable={['client', 'plugin_type', 'scope']}
							onClickRow={(d, isCtrlClick) => {
								openUrl(`/admin/device-scans/${scanId}/plugins/${d.index}`, isCtrlClick);
							}}
						>
							{#snippet onRenderColumn(property, d: PluginRow)}
								{#if property === 'enabled'}
									{d.enabled ? 'yes' : 'no'}
								{:else if property === 'version'}
									<span class="font-mono text-xs">{d.version ?? '—'}</span>
								{:else}
									{d[property as keyof PluginRow] ?? '—'}
								{/if}
							{/snippet}
						</Table>
					{/if}
				{:else if activeTab === 'files'}
					{#if fileRows.length === 0}
						{@render emptyTab('No files captured in this scan.')}
					{:else}
						<Table
							data={fileRows}
							fields={['path', 'size_display', 'oversized']}
							headers={[
								{ title: 'Path', property: 'path' },
								{ title: 'Size', property: 'size_display' },
								{ title: 'Oversized', property: 'oversized' }
							]}
							sortable={['path']}
						>
							{#snippet onRenderColumn(property, d: FileRow)}
								{#if property === 'path'}
									<span class="font-mono text-xs">{d.path}</span>
								{:else if property === 'oversized'}
									{d.oversized ? 'yes' : 'no'}
								{:else}
									{d[property as keyof FileRow] ?? '—'}
								{/if}
							{/snippet}
						</Table>
					{/if}
				{/if}
			</div>
		{/if}
	</div>
</Layout>

{#snippet emptyTab(msg: string)}
	<div class="text-on-surface1 flex items-center gap-2 p-4 text-sm font-light">
		<Cpu class="size-4 opacity-50" />
		{msg}
	</div>
{/snippet}

<style lang="postcss">
	.tab-button {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 0.75rem;
		border-bottom: 2px solid transparent;
		font-size: 0.875rem;
		color: var(--on-surface1, #6b7280);
		transition:
			color 200ms,
			border-color 200ms;

		&:hover {
			color: inherit;
		}
	}
	.tab-active {
		color: inherit;
		border-bottom-color: var(--primary);
		font-weight: 500;
	}
</style>
