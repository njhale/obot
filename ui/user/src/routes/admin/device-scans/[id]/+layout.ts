import { handleRouteError } from '$lib/errors';
import { AdminService } from '$lib/services';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ params, fetch, parent }) => {
	const { id } = params;
	const { profile } = await parent();

	try {
		const scan = await AdminService.getDeviceScan(id, { fetch });
		return { scan };
	} catch (err) {
		handleRouteError(err, `/admin/device-scans/${id}`, profile);
	}
};
