import type { RomDiscEntry } from '#lib/api/types.js';

/**
 * The format a ROM's file is stored in, as people name it: CHD or CUE/BIN or GDI for a disc
 * image, otherwise the file's extension ("SFC", "ISO"). Empty when nothing says.
 */
export function fileFormat(entry: RomDiscEntry): string {
	if (entry.disc?.format === 'chd') return 'CHD';
	if (entry.disc?.format === 'tracks') {
		const gdi = /\.gdi$/i.test(entry.filename) || entry.downloads?.some((option) => option.sheet === 'gdi');
		return gdi ? 'GDI' : 'CUE/BIN';
	}
	const extension = /\.([^./\\]+)$/.exec(entry.filename)?.[1] ?? entry.downloads?.[0]?.extension.replace(/^\./, '') ?? '';
	return extension.toUpperCase();
}

/** The bytes every file of a title takes together. */
export function totalSize(entries: RomDiscEntry[]): number {
	return entries.reduce((sum, entry) => sum + entry.size, 0);
}
