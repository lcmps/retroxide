const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

/** Formats a byte count with binary units (1 KB = 1024 B), e.g. "1.5 GB". */
export function formatBytes(bytes: number, locale?: string): string {
	let value = Math.max(0, bytes);
	let unit = 0;
	while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
		value /= 1024;
		unit += 1;
	}
	const digits = unit === 0 || value >= 100 ? 0 : 1;
	return `${new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(value)} ${BYTE_UNITS[unit]}`;
}
