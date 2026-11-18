import { describe, expect, it, vi } from 'vitest';
import type { RomListResponse, RomSummary } from '#lib/api/types.js';
import { RomPager } from './pager.svelte.js';

const rom = (id: string): RomSummary => ({ rom_id: id, title: id, platform: 'snes', platform_name: 'SNES', sha256: id, cover_art_url: null, disc_count: 1 });

describe('RomPager', () => {
	it('appends pages until the server reports no further cursor', async () => {
		const fetchPage = vi
			.fn<(limit: number, cursor: string | undefined) => Promise<RomListResponse>>()
			.mockResolvedValueOnce({ item: [rom('a'), rom('b')], next_cursor: 'b' })
			.mockResolvedValueOnce({ item: [rom('c')], next_cursor: null });
		const pager = new RomPager(fetchPage, 2);

		await pager.reset({ q: 'x' });
		await pager.loadMore();
		await pager.loadMore(); // exhausted: no further request

		expect(pager.items.map((item) => item.rom_id)).toEqual(['a', 'b', 'c']);
		expect(pager.exhausted).toBe(true);
		expect(fetchPage).toHaveBeenCalledTimes(2);
		expect(fetchPage).toHaveBeenNthCalledWith(2, 2, 'b', { q: 'x' });
	});

	it('ignores a page that arrives after the filters changed', async () => {
		let resolveStale: (page: RomListResponse) => void = () => undefined;
		const fetchPage = vi
			.fn<(limit: number, cursor: string | undefined) => Promise<RomListResponse>>()
			.mockImplementationOnce(() => new Promise((resolve) => (resolveStale = resolve)))
			.mockResolvedValueOnce({ item: [rom('fresh')], next_cursor: null });
		const pager = new RomPager(fetchPage, 10);

		const stale = pager.reset({ platform: 'nes' });
		await pager.reset({ platform: 'snes' });
		resolveStale({ item: [rom('stale')], next_cursor: null });
		await stale;

		expect(pager.items.map((item) => item.rom_id)).toEqual(['fresh']);
		expect(pager.loading).toBe(false);
	});

	it('stops loading after an error until reset', async () => {
		const fetchPage = vi.fn<(limit: number, cursor: string | undefined) => Promise<RomListResponse>>().mockRejectedValue(new Error('offline'));
		const pager = new RomPager(fetchPage, 10);

		await pager.reset();
		await pager.loadMore();

		expect(pager.error).toBeInstanceOf(Error);
		expect(fetchPage).toHaveBeenCalledTimes(1);
	});
});

describe('RomPager total', () => {
	it('keeps the first page total while paging and clears it on reset', async () => {
		let resolveSecond: (page: RomListResponse) => void = () => undefined;
		const fetchPage = vi
			.fn<(limit: number, cursor: string | undefined) => Promise<RomListResponse>>()
			.mockResolvedValueOnce({ item: [rom('a')], next_cursor: 'a', total: 3 })
			.mockResolvedValueOnce({ item: [rom('b')], next_cursor: null, total: null })
			.mockImplementationOnce(() => new Promise((resolve) => (resolveSecond = resolve)));
		const pager = new RomPager(fetchPage, 1);

		await pager.reset({});
		expect(pager.total).toBe(3);
		await pager.loadMore();
		expect(pager.total).toBe(3);

		const pending = pager.reset({ q: 'zelda' });
		expect(pager.total).toBeNull();
		resolveSecond({ item: [], next_cursor: null, total: 0 });
		await pending;
		expect(pager.total).toBe(0);
	});
});
