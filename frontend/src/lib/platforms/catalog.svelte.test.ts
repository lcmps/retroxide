import { describe, expect, it, vi } from 'vitest';
import type { Platform } from '#lib/api/types.js';
import { PlatformCatalog } from './catalog.svelte.js';

const sms: Platform = { key: 'sms', name: 'Master System', rom_count: 2, bios_count: 0 };
const pico: Platform = { key: 'Sega Pico', name: 'Sega Pico', rom_count: 1, bios_count: 0 };

describe('PlatformCatalog', () => {
	it('loads platforms and resolves display names case-insensitively', async () => {
		const catalog = new PlatformCatalog(async () => [sms, pico]);
		expect(catalog.nameFor('sms')).toBe('sms');

		await catalog.refresh();

		expect(catalog.items).toEqual([sms, pico]);
		expect(catalog.nameFor('SMS')).toBe('Master System');
		expect(catalog.nameFor('sega pico')).toBe('Sega Pico');
		expect(catalog.nameFor('vita')).toBe('vita');
	});

	it('coalesces concurrent refreshes and keeps the last list when loading fails', async () => {
		let resolve: (items: Platform[]) => void = () => {};
		const load = vi
			.fn<() => Promise<Platform[]>>()
			.mockImplementationOnce(() => new Promise((done) => (resolve = done)))
			.mockRejectedValueOnce(new Error('offline'));
		const catalog = new PlatformCatalog(load);

		const first = catalog.refresh();
		const second = catalog.refresh();
		resolve([sms]);
		await Promise.all([first, second]);
		expect(load).toHaveBeenCalledTimes(1);

		await catalog.refresh();
		expect(load).toHaveBeenCalledTimes(2);
		expect(catalog.items).toEqual([sms]);
	});
});

describe('platforms with only BIOS files', () => {
	it('are listed, so their BIOS files can be reached', async () => {
		const ps2: Platform = { key: 'ps2', name: 'PlayStation 2', rom_count: 0, bios_count: 1 };
		const catalog = new PlatformCatalog(async () => [ps2]);
		await catalog.refresh();
		expect(catalog.items).toEqual([ps2]);
		expect(catalog.nameFor('ps2')).toBe('PlayStation 2');
	});
});
