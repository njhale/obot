import { handleRouteError } from '$lib/errors';
import { AdminService } from '$lib/services';
import type {
	AggregatedDeviceMCPServerFilters,
	AggregatedDeviceMCPServerList,
	AggregatedDeviceMCPServerSortKey
} from '$lib/services/admin/types';
import { profile } from '$lib/stores';
import type { PageLoad } from './$types';

const PAGE_SIZE = 50;

const SORTABLE: AggregatedDeviceMCPServerSortKey[] = [
	'name',
	'device_count',
	'user_count',
	'client_count',
	'first_seen',
	'last_seen'
];

function pickSortBy(raw: string | null): AggregatedDeviceMCPServerSortKey | undefined {
	if (!raw) return undefined;
	return SORTABLE.includes(raw as AggregatedDeviceMCPServerSortKey)
		? (raw as AggregatedDeviceMCPServerSortKey)
		: undefined;
}

function defaultRange(): { start: string; end: string } {
	const end = new Date();
	const start = new Date(end.getTime() - 60 * 24 * 60 * 60 * 1000);
	return { start: start.toISOString(), end: end.toISOString() };
}

export const load: PageLoad = async ({
	url,
	fetch
}: {
	url: URL;
	fetch: typeof globalThis.fetch;
}) => {
	const q = url.searchParams;
	const explicitStart = q.get('start');
	const explicitEnd = q.get('end');
	const range =
		explicitStart && explicitEnd ? { start: explicitStart, end: explicitEnd } : defaultRange();

	const filters: AggregatedDeviceMCPServerFilters = {
		limit: PAGE_SIZE,
		offset: parseInt(q.get('offset') ?? '0', 10) || 0,
		start: range.start,
		end: range.end,
		name: q.get('name') ?? undefined,
		command: q.get('command') ?? undefined,
		url: q.get('url') ?? undefined,
		transport: q
			.getAll('transport')
			.flatMap((v: string) => v.split(','))
			.filter(Boolean),
		client: q
			.getAll('client')
			.flatMap((v: string) => v.split(','))
			.filter(Boolean),
		sortBy: pickSortBy(q.get('sort_by')),
		sortOrder: (q.get('sort_order') as 'asc' | 'desc' | null) ?? undefined
	};

	let servers: AggregatedDeviceMCPServerList = { items: [], total: 0, limit: PAGE_SIZE, offset: 0 };
	let transportOptions: string[] = [];
	let clientOptions: string[] = [];

	try {
		[servers, transportOptions, clientOptions] = await Promise.all([
			AdminService.listAggregatedDeviceMCPServers(filters, { fetch }),
			AdminService.listDeviceMCPServerFilterOptions('transport', range, { fetch }),
			AdminService.listDeviceMCPServerFilterOptions('client', range, { fetch })
		]);
		return {
			servers,
			transportOptions,
			clientOptions,
			range,
			filters,
			pageSize: PAGE_SIZE
		};
	} catch (err) {
		handleRouteError(err, '/admin/device-mcp-servers', profile.current);
	}
};
