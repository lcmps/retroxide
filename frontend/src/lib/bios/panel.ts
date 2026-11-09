import type { Bios, BiosUploadResult } from '#lib/api/types.js';

/** What the collapsed BIOS line says: how many files, and how many still need a label. */
export function collapsedSummary(entries: Bios[]): { count: number; needsLabel: number } {
	const needsLabel = entries.filter((entry) => !entry.recognized && !hasLabels(entry)).length;
	return { count: entries.length, needsLabel };
}

/** A short name for a BIOS file: its labels, or its filename when it has none. */
export function biosLabel(entry: Bios): string {
	const labels = [entry.region, entry.version, entry.model].filter(Boolean);
	return labels.length > 0 ? labels.join(' · ') : entry.filename;
}

/** The parts of an upload's result message: "Added as {platform} BIOS ({details})". */
export function uploadResultLabel(result: BiosUploadResult): { platform: string; details: string; recognized: boolean } {
	return {
		platform: result.platform_name,
		details: [result.region, result.version].filter(Boolean).join(', '),
		recognized: result.recognized
	};
}

function hasLabels(entry: Bios): boolean {
	return Boolean(entry.region || entry.version || entry.model || entry.date || entry.note);
}
