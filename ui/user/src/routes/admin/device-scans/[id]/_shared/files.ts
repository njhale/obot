import type { DeviceScan, DeviceScanFile } from '$lib/services/admin/types';

export function lookupFiles(
	scanFiles: DeviceScanFile[] | undefined,
	paths: string[] | undefined
): { path: string; file?: DeviceScanFile }[] {
	const byPath = new Map<string, DeviceScanFile>();
	for (const f of scanFiles ?? []) byPath.set(f.path, f);
	return (paths ?? []).map((path) => ({ path, file: byPath.get(path) }));
}

export function formatBytes(n: number): string {
	if (n < 1024) return `${n} B`;
	if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
	return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

export function shortHash(h?: string): string {
	if (!h) return '—';
	return h.length > 12 ? `${h.slice(0, 8)}…${h.slice(-4)}` : h;
}

export function findParentPlugin(
	scan: DeviceScan | undefined,
	pluginFile: string | undefined
): { index: number; name: string } | undefined {
	if (!scan || !pluginFile) return undefined;
	const idx = scan.plugins?.findIndex((p) => p.files?.includes(pluginFile)) ?? -1;
	if (idx < 0) return undefined;
	return { index: idx, name: scan.plugins[idx].name };
}
