import type { RomSortField, RomSortOrder } from '#lib/api/client.js';

/** The library's order; null field is the default order (by ID, as added). */
export type RomSort = { field: RomSortField; order: RomSortOrder } | null;

export const SORT_FIELDS: readonly RomSortField[] = ['title', 'release', 'publisher', 'genre'];

function isSortField(value: string | null): value is RomSortField {
	return value !== null && (SORT_FIELDS as readonly string[]).includes(value);
}

/** Reads the order from a URL's sort and order parameters; anything unknown is the default. */
export function sortFromParams(params: Pick<URLSearchParams, 'get'>): RomSort {
	const field = params.get('sort');
	if (!isSortField(field)) return null;
	return { field, order: params.get('order') === 'desc' ? 'desc' : 'asc' };
}

/** Writes the order into params, removing both parameters for the default order. */
export function applySortToParams(params: Pick<URLSearchParams, 'toString'>, sort: RomSort): URLSearchParams {
	const next = new URLSearchParams(params.toString());
	if (sort) {
		next.set('sort', sort.field);
		next.set('order', sort.order);
	} else {
		next.delete('sort');
		next.delete('order');
	}
	return next;
}

/**
 * The order after selecting field: another field starts ascending; the active one turns
 * descending, then back to the default order.
 */
export function nextSort(current: RomSort, field: RomSortField): RomSort {
	if (current?.field !== field) return { field, order: 'asc' };
	return current.order === 'asc' ? { field, order: 'desc' } : null;
}
