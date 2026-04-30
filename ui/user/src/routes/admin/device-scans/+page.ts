import { handleRouteError } from '$lib/errors';
import { AdminService } from '$lib/services';
import type { DeviceScanList } from '$lib/services/admin/types';
import { profile } from '$lib/stores';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	let scans: DeviceScanList = { items: [], total: 0, limit: 50, offset: 0 };

	try {
		scans = await AdminService.listDeviceScans({ limit: 50, groupByDevice: false }, { fetch });
		return { scans };
	} catch (err) {
		handleRouteError(err, '/admin/device-scans', profile.current);
	}
};
