import { describe, expect, it } from 'vitest';
import { applySortToParams, nextSort, sortFromParams } from './sort.js';

describe('library sort', () => {
	it('cycles a field through ascending, descending, and the default order', () => {
		const ascending = nextSort(null, 'title');
		expect(ascending).toEqual({ field: 'title', order: 'asc' });
		const descending = nextSort(ascending, 'title');
		expect(descending).toEqual({ field: 'title', order: 'desc' });
		expect(nextSort(descending, 'title')).toBeNull();
	});

	it('starts another field ascending', () => {
		expect(nextSort({ field: 'title', order: 'desc' }, 'genre')).toEqual({ field: 'genre', order: 'asc' });
	});

	it('round-trips through URL parameters and keeps the others', () => {
		const params = applySortToParams(new URLSearchParams('q=mario&platform=snes'), { field: 'release', order: 'desc' });
		expect(params.get('q')).toBe('mario');
		expect(sortFromParams(params)).toEqual({ field: 'release', order: 'desc' });
		const cleared = applySortToParams(params, null);
		expect(cleared.has('sort') || cleared.has('order')).toBe(false);
		expect(cleared.get('platform')).toBe('snes');
	});

	it('treats unknown values as the default order and a missing order as ascending', () => {
		expect(sortFromParams(new URLSearchParams('sort=rating&order=desc'))).toBeNull();
		expect(sortFromParams(new URLSearchParams('sort=genre&order=sideways'))).toEqual({ field: 'genre', order: 'asc' });
	});
});
