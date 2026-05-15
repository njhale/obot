export function previewLine(text: string, maxLength: number): string {
	if (!text) return '';
	const collapsed = text.replace(/\s+/g, ' ').trim();
	if (collapsed.length <= maxLength) return collapsed;
	return collapsed.slice(0, maxLength) + '…';
}
