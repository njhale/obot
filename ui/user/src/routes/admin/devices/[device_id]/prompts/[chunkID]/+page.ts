import { handleRouteError } from '$lib/errors';
import { AdminService } from '$lib/services';
import type { DeviceScanPrompt } from '$lib/services/admin/types';
import { profile } from '$lib/stores';
import type { PageLoad } from './$types';
import { error } from '@sveltejs/kit';

export const load: PageLoad = async ({ params, fetch }) => {
	const { device_id, chunkID } = params;

	try {
		const resp = await AdminService.getDevicePromptsLatest(device_id, undefined, { fetch });
		const prompt: DeviceScanPrompt | undefined = (resp.items ?? []).find(
			(p) => p.chunkID === chunkID
		);
		if (!prompt) {
			throw error(404, `Prompt ${chunkID} not found for device ${device_id}.`);
		}
		return { prompt, deviceId: device_id };
	} catch (err) {
		handleRouteError(err, `/admin/devices/${device_id}/prompts/${chunkID}`, profile.current);
	}
};
