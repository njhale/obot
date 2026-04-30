<script lang="ts">
	import { page } from '$app/state';
	import Layout from '$lib/components/Layout.svelte';
	import AuditLogCalendar from '$lib/components/admin/audit-logs/AuditLogCalendar.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import { PAGE_TRANSITION_DURATION } from '$lib/constants';
	import {
		AdminService,
		type AggregatedDeviceMCPServer,
		type AggregatedDeviceMCPServerList,
		type AggregatedDeviceMCPServerSortKey
	} from '$lib/services';
	import { formatTimeAgo } from '$lib/time';
	import { replaceState } from '$lib/url';
	import { openUrl } from '$lib/utils';
	import ColumnPicker from './_shared/ColumnPicker.svelte';
	import FiltersBar from './_shared/FiltersBar.svelte';
	import { ChevronsLeft, ChevronsRight, Server } from 'lucide-svelte';
	import { untrack } from 'svelte';
	import { fly } from 'svelte/transition';

	let { data } = $props();
	const PAGE_SIZE = untrack(() => data?.pageSize ?? 50);

	let serversResponse = $state<AggregatedDeviceMCPServerList>(
		untrack(() => data?.servers ?? { items: [], total: 0, limit: PAGE_SIZE, offset: 0 })
	);
	let transportOptions = $state<string[]>(untrack(() => data?.transportOptions ?? []));
	let clientOptions = $state<string[]>(untrack(() => data?.clientOptions ?? []));
	let pageIndex = $state(untrack(() => Math.floor((data?.filters?.offset ?? 0) / PAGE_SIZE)));
	let loading = $state(false);

	let range = $state<{ start: string; end: string }>(
		untrack(
			() =>
				data?.range ?? {
					start: new Date(Date.now() - 60 * 24 * 60 * 60 * 1000).toISOString(),
					end: new Date().toISOString()
				}
		)
	);

	let nameFilter = $state(untrack(() => data?.filters?.name ?? ''));
	let commandFilter = $state(untrack(() => data?.filters?.command ?? ''));
	let urlFilter = $state(untrack(() => data?.filters?.url ?? ''));
	let transports = $state<string[]>(untrack(() => data?.filters?.transport ?? []));
	let clients = $state<string[]>(untrack(() => data?.filters?.client ?? []));
	let sortBy = $state<AggregatedDeviceMCPServerSortKey>(
		untrack(() => data?.filters?.sortBy ?? 'device_count')
	);
	let sortOrder = $state<'asc' | 'desc'>(untrack(() => data?.filters?.sortOrder ?? 'desc'));

	const ALL_COLUMNS = [
		{ id: 'name', label: 'Name', alwaysVisible: true },
		{ id: 'transport', label: 'Transport' },
		{ id: 'command', label: 'Command' },
		{ id: 'device_count', label: 'Devices' },
		{ id: 'user_count', label: 'Users' },
		{ id: 'client_count', label: 'Clients' },
		{ id: 'last_seen', label: 'Last Seen' },
		{ id: 'first_seen', label: 'First Seen' },
		{ id: 'observation_count', label: 'Observations' },
		{ id: 'scope_count', label: 'Scopes' },
		{ id: 'url', label: 'URL' },
		{ id: 'config_hash', label: 'ConfigHash' }
	];

	const DEFAULT_VISIBLE = [
		'name',
		'transport',
		'command',
		'device_count',
		'user_count',
		'client_count',
		'last_seen'
	];

	let visibleColumns = $state<string[]>(parseColumnsFromUrl() ?? [...DEFAULT_VISIBLE]);

	function parseColumnsFromUrl(): string[] | undefined {
		const cols = page.url.searchParams.get('cols');
		if (!cols) return undefined;
		return cols.split(',').filter(Boolean);
	}

	function persistColumnsToUrl(cols: string[]) {
		const sorted = [...cols].sort();
		const defaultSorted = [...DEFAULT_VISIBLE].sort();
		const isDefault =
			sorted.length === defaultSorted.length && sorted.every((c, i) => c === defaultSorted[i]);
		const next = new URL(page.url);
		if (isDefault) next.searchParams.delete('cols');
		else next.searchParams.set('cols', sorted.join(','));
		replaceState(next, { cols: sorted });
	}

	type Row = AggregatedDeviceMCPServer & {
		id: string;
		display_name: string;
		display_command: string;
		last_seen_relative: string;
		first_seen_relative: string;
		short_hash: string;
	};

	let rows = $derived<Row[]>(
		(serversResponse.items ?? []).map((s) => ({
			...s,
			id: s.config_hash,
			display_name: s.name?.trim() ? s.name : '(unnamed)',
			display_command: s.command ?? '',
			last_seen_relative: formatTimeAgo(s.last_seen).relativeTime,
			first_seen_relative: formatTimeAgo(s.first_seen).relativeTime,
			short_hash: (s.config_hash ?? '').slice(0, 10)
		}))
	);

	let total = $derived(serversResponse.total ?? 0);
	let lastPageIndex = $derived(total > 0 ? Math.ceil(total / PAGE_SIZE) - 1 : 0);

	let visibleFields = $derived(
		ALL_COLUMNS.filter((c) => c.alwaysVisible || visibleColumns.includes(c.id)).map((c) =>
			fieldFor(c.id)
		)
	);

	let visibleHeaders = $derived(
		ALL_COLUMNS.filter((c) => c.alwaysVisible || visibleColumns.includes(c.id)).map((c) => ({
			title: c.label,
			property: fieldFor(c.id)
		}))
	);

	function fieldFor(colId: string): string {
		switch (colId) {
			case 'name':
				return 'display_name';
			case 'command':
				return 'display_command';
			case 'last_seen':
				return 'last_seen_relative';
			case 'first_seen':
				return 'first_seen_relative';
			case 'config_hash':
				return 'short_hash';
			default:
				return colId;
		}
	}

	const SORTABLE_COLUMNS = new Set([
		'display_name',
		'device_count',
		'user_count',
		'client_count',
		'first_seen_relative',
		'last_seen_relative'
	]);

	let sortableFields = $derived(visibleFields.filter((f) => SORTABLE_COLUMNS.has(f)));

	function fieldToSortKey(field: string): AggregatedDeviceMCPServerSortKey {
		switch (field) {
			case 'display_name':
				return 'name';
			case 'last_seen_relative':
				return 'last_seen';
			case 'first_seen_relative':
				return 'first_seen';
			default:
				return field as AggregatedDeviceMCPServerSortKey;
		}
	}

	function sortKeyToField(key: AggregatedDeviceMCPServerSortKey): string {
		switch (key) {
			case 'name':
				return 'display_name';
			case 'last_seen':
				return 'last_seen_relative';
			case 'first_seen':
				return 'first_seen_relative';
			default:
				return key;
		}
	}

	let initSort = $derived({
		property: sortKeyToField(sortBy),
		order: sortOrder as 'asc' | 'desc'
	});

	function syncUrl() {
		const next = new URL(page.url);
		const params = next.searchParams;
		params.delete('start');
		params.delete('end');
		params.delete('name');
		params.delete('command');
		params.delete('url');
		params.delete('transport');
		params.delete('client');
		params.delete('sort_by');
		params.delete('sort_order');
		params.delete('offset');
		// only set start/end when user customised — defaults stay clean
		const defaultStart = new Date(Date.now() - 60 * 24 * 60 * 60 * 1000).getTime();
		const startMs = new Date(range.start).getTime();
		const endMs = new Date(range.end).getTime();
		if (Math.abs(startMs - defaultStart) > 60_000 || Math.abs(endMs - Date.now()) > 60_000) {
			params.set('start', range.start);
			params.set('end', range.end);
		}
		if (nameFilter) params.set('name', nameFilter);
		if (commandFilter) params.set('command', commandFilter);
		if (urlFilter) params.set('url', urlFilter);
		if (transports.length > 0) params.set('transport', transports.join(','));
		if (clients.length > 0) params.set('client', clients.join(','));
		if (sortBy !== 'device_count') params.set('sort_by', sortBy);
		if (sortOrder !== 'desc') params.set('sort_order', sortOrder);
		if (pageIndex > 0) params.set('offset', String(pageIndex * PAGE_SIZE));
		replaceState(next, {});
	}

	async function reload() {
		loading = true;
		try {
			serversResponse = await AdminService.listAggregatedDeviceMCPServers({
				limit: PAGE_SIZE,
				offset: pageIndex * PAGE_SIZE,
				start: range.start,
				end: range.end,
				name: nameFilter || undefined,
				command: commandFilter || undefined,
				url: urlFilter || undefined,
				transport: transports.length > 0 ? transports : undefined,
				client: clients.length > 0 ? clients : undefined,
				sortBy,
				sortOrder
			});
		} finally {
			loading = false;
		}
		syncUrl();
	}

	async function reloadFilterOptions() {
		[transportOptions, clientOptions] = await Promise.all([
			AdminService.listDeviceMCPServerFilterOptions('transport', range),
			AdminService.listDeviceMCPServerFilterOptions('client', range)
		]);
	}

	function onRangeChange({ start, end }: { start: Date | string; end: Date | string }) {
		range = {
			start: new Date(start).toISOString(),
			end: new Date(end).toISOString()
		};
		pageIndex = 0;
		reload();
		reloadFilterOptions();
	}

	function onFiltersChange(next: {
		name: string;
		command: string;
		url: string;
		transports: string[];
		clients: string[];
	}) {
		nameFilter = next.name;
		commandFilter = next.command;
		urlFilter = next.url;
		transports = next.transports;
		clients = next.clients;
		pageIndex = 0;
		reload();
	}

	function onSort(property: string, order: 'asc' | 'desc') {
		sortBy = fieldToSortKey(property);
		sortOrder = order;
		pageIndex = 0;
		reload();
	}

	function onColumnsChange(next: string[]) {
		visibleColumns = next;
		persistColumnsToUrl(next);
	}

	function fetchPage(idx: number) {
		pageIndex = idx;
		reload();
	}

	const duration = PAGE_TRANSITION_DURATION;
</script>

<svelte:head>
	<title>Obot | MCP Servers</title>
</svelte:head>

<Layout title="MCP Servers">
	<div
		class="flex h-full w-full flex-col gap-4"
		in:fly={{ x: 100, duration, delay: duration }}
		out:fly={{ x: -100, duration }}
	>
		<div class="flex flex-wrap items-center gap-2">
			<AuditLogCalendar
				start={new Date(range.start)}
				end={new Date(range.end)}
				onChange={onRangeChange}
			/>
			<FiltersBar
				name={nameFilter}
				command={commandFilter}
				url={urlFilter}
				{transports}
				{clients}
				{transportOptions}
				{clientOptions}
				onChange={onFiltersChange}
			/>
			<div class="grow"></div>
			<ColumnPicker columns={ALL_COLUMNS} visible={visibleColumns} onChange={onColumnsChange} />
		</div>

		{#if total === 0 && !loading}
			<div class="mt-12 flex w-md flex-col items-center gap-4 self-center text-center">
				<Server class="text-on-surface1 size-24 opacity-50" />
				<h4 class="text-on-surface1 text-lg font-semibold">No MCP servers in this window</h4>
				<p class="text-on-surface1 text-sm font-light">
					Adjust the date range or run <code class="font-mono">obot scan</code> from a managed device.
				</p>
			</div>
		{:else}
			<Table
				data={rows}
				fields={visibleFields}
				headers={visibleHeaders}
				sortable={sortableFields}
				{initSort}
				{onSort}
				onClickRow={(d, isCtrlClick) => {
					openUrl(`/admin/device-mcp-servers/${encodeURIComponent(d.config_hash)}`, isCtrlClick);
				}}
			>
				{#snippet onRenderColumn(property, d: Row)}
					{#if property === 'display_name'}
						{#if d.name?.trim()}
							{d.name}
						{:else}
							<span class="text-on-surface2 italic">(unnamed)</span>
						{/if}
					{:else if property === 'display_command'}
						<code class="font-mono text-xs">{d.command || '—'}</code>
					{:else if property === 'short_hash'}
						<code class="font-mono text-xs">{d.short_hash}</code>
					{:else if property === 'url'}
						<span class="font-mono text-xs">{d.url || ''}</span>
					{:else}
						{d[property as keyof Row]}
					{/if}
				{/snippet}
			</Table>

			{#if total > PAGE_SIZE}
				<div class="flex items-center justify-center gap-4 pt-2">
					<button
						class="button-text flex items-center gap-1 text-xs"
						disabled={pageIndex === 0 || loading}
						onclick={() => fetchPage(pageIndex - 1)}
					>
						<ChevronsLeft class="size-4" /> Previous
					</button>
					<p class="text-on-surface1 text-xs">
						{pageIndex + 1} of {lastPageIndex + 1} · {total} server{total === 1 ? '' : 's'}
					</p>
					<button
						class="button-text flex items-center gap-1 text-xs"
						disabled={pageIndex >= lastPageIndex || loading}
						onclick={() => fetchPage(pageIndex + 1)}
					>
						Next <ChevronsRight class="size-4" />
					</button>
				</div>
			{/if}
		{/if}
	</div>
</Layout>
