<script lang="ts">
	import { page } from '$app/state';
	import Layout from '$lib/components/Layout.svelte';
	import Search from '$lib/components/Search.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import { PAGE_TRANSITION_DURATION } from '$lib/constants';
	import { AdminService, type DeviceScan, type DeviceScanList } from '$lib/services';
	import { formatTimeAgo } from '$lib/time';
	import {
		clearUrlParams,
		getTableUrlParamsFilters,
		getTableUrlParamsSort,
		setSortUrlParams,
		setFilterUrlParams,
		replaceState
	} from '$lib/url';
	import { openUrl } from '$lib/utils';
	import { debounce } from 'es-toolkit';
	import { ChevronsLeft, ChevronsRight, ScanLine } from 'lucide-svelte';
	import { untrack } from 'svelte';
	import { fly } from 'svelte/transition';

	let { data } = $props();

	const PAGE_SIZE = 50;

	let scansResponse = $state<DeviceScanList>(
		untrack(() => data?.scans ?? { items: [], total: 0, limit: PAGE_SIZE, offset: 0 })
	);
	let pageIndex = $state(0);
	let loading = $state(false);
	let query = $derived(page.url.searchParams.get('query') || '');

	async function fetchPage(idx: number) {
		loading = true;
		try {
			scansResponse = await AdminService.listDeviceScans({
				limit: PAGE_SIZE,
				offset: idx * PAGE_SIZE,
				groupByDevice: false
			});
			pageIndex = idx;
		} finally {
			loading = false;
		}
	}

	const updateQuery = debounce((value: string) => {
		if (value) {
			page.url.searchParams.set('query', value);
		} else {
			page.url.searchParams.delete('query');
		}
		replaceState(page.url, { query: value });
	}, 100);

	type Row = DeviceScan & {
		os_arch: string;
		mcp_count: number;
		skill_count: number;
		plugin_count: number;
		scanned_relative: string;
	};

	let rows = $derived<Row[]>(
		(scansResponse.items ?? []).map((s) => ({
			...s,
			os_arch: `${s.os} / ${s.arch}`,
			mcp_count: s.mcp_servers?.length ?? 0,
			skill_count: s.skills?.length ?? 0,
			plugin_count: s.plugins?.length ?? 0,
			scanned_relative: formatTimeAgo(s.scanned_at).relativeTime
		}))
	);

	let filteredRows = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return rows;
		return rows.filter(
			(r) => r.device_id?.toLowerCase().includes(q) || (r.username ?? '').toLowerCase().includes(q)
		);
	});

	let total = $derived(scansResponse.total ?? 0);
	let lastPageIndex = $derived(total > 0 ? Math.ceil(total / PAGE_SIZE) - 1 : 0);

	let urlFilters = $derived(getTableUrlParamsFilters());
	let initSort = $derived(getTableUrlParamsSort());

	const duration = PAGE_TRANSITION_DURATION;
</script>

<svelte:head>
	<title>Obot | Device Scans</title>
</svelte:head>

<Layout title="Device Scans">
	<div
		class="h-full w-full"
		in:fly={{ x: 100, duration, delay: duration }}
		out:fly={{ x: -100, duration }}
	>
		{#if total === 0 && !loading}
			<div class="mx-auto mt-12 flex w-md flex-col items-center gap-4 text-center">
				<ScanLine class="text-on-surface1 size-24 opacity-50" />
				<h4 class="text-on-surface1 text-lg font-semibold">No device scans yet</h4>
				<p class="text-on-surface1 text-sm font-light">
					Run <code class="font-mono">obot scan</code> from a managed device to populate this view.
				</p>
			</div>
		{:else}
			<div class="flex flex-col gap-2">
				<Search
					value={query}
					class="dark:bg-surface1 dark:border-surface3 bg-background border border-transparent shadow-sm"
					onChange={updateQuery}
					placeholder="Search by device ID or user..."
				/>

				<Table
					data={filteredRows}
					fields={[
						'device_id',
						'os_arch',
						'username',
						'mcp_count',
						'skill_count',
						'plugin_count',
						'scanner_version',
						'scanned_relative'
					]}
					onClickRow={(d, isCtrlClick) => {
						openUrl(`/admin/device-scans/${d.id}`, isCtrlClick);
					}}
					filterable={['os_arch']}
					filters={urlFilters}
					onFilter={setFilterUrlParams}
					onClearAllFilters={clearUrlParams}
					sortable={['device_id', 'os_arch', 'username', 'scanner_version']}
					onSort={setSortUrlParams}
					{initSort}
					headers={[
						{ title: 'Device ID', property: 'device_id' },
						{ title: 'OS / Arch', property: 'os_arch' },
						{ title: 'User', property: 'username' },
						{ title: 'MCP', property: 'mcp_count' },
						{ title: 'Skills', property: 'skill_count' },
						{ title: 'Plugins', property: 'plugin_count' },
						{ title: 'Scanner', property: 'scanner_version' },
						{ title: 'Scanned', property: 'scanned_relative' }
					]}
				>
					{#snippet onRenderColumn(property, d: Row)}
						{#if property === 'device_id'}
							<span class="font-mono text-xs">{d.device_id}</span>
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
							{pageIndex + 1} of {lastPageIndex + 1} · {total} device{total === 1 ? '' : 's'}
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
			</div>
		{/if}
	</div>
</Layout>
