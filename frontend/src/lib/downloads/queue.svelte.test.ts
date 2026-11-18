import { describe, expect, it, vi } from 'vitest';
import type { RomSummary } from '#lib/api/types.js';
import { DownloadQueue, DOWNLOAD_QUEUE_STORAGE_KEY } from './queue.svelte.js';

const rom = (id: string): RomSummary => ({ rom_id: id, title: `Game ${id}`, platform: 'snes', platform_name: 'SNES', sha256: id, cover_art_url: null, disc_count: 1 });

function memoryStorage() {
	const values = new Map<string, string>();
	return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => void values.set(key, value), values };
}

describe('DownloadQueue', () => {
	it('adds each ROM once, newest first, and reports what is queued', () => {
		const queue = new DownloadQueue(vi.fn(), memoryStorage());
		queue.add(rom('a'));
		queue.add(rom('b'));
		queue.add(rom('a'));
		expect(queue.items.map((item) => item.romId)).toEqual(['b', 'a']);
		expect(queue.has('a')).toBe(true);
		queue.remove('a');
		expect(queue.has('a')).toBe(false);
	});

	it('downloads every ready or failed item in order, waiting between, and goes on past a failure', async () => {
		const download = vi.fn(async (romId: string) => {
			if (romId === 'gone') throw new Error('This ROM no longer exists');
			return { filename: `${romId}.sfc` };
		});
		const wait = vi.fn(async () => {});
		const queue = new DownloadQueue(download, memoryStorage(), wait);
		for (const id of ['c', 'gone', 'a']) queue.add(rom(id));
		await queue.startAll();

		expect(download.mock.calls.map(([id]) => id)).toEqual(['a', 'gone', 'c']);
		expect(wait).toHaveBeenCalledTimes(2);
		expect(wait).toHaveBeenCalledWith(1000);
		const gone = queue.items.find((item) => item.romId === 'gone')!;
		expect(gone.status).toBe('failed');
		expect(gone.error).toBe('This ROM no longer exists');
		expect(queue.items.find((item) => item.romId === 'c')).toMatchObject({ status: 'completed', filename: 'c.sfc' });

		// A second round retries only what failed.
		download.mockClear();
		await queue.startAll();
		expect(download.mock.calls.map(([id]) => id)).toEqual(['gone']);
	});

	it('clears only completed items', async () => {
		const queue = new DownloadQueue(async (id) => ({ filename: id }), memoryStorage(), async () => {});
		queue.add(rom('a'));
		await queue.start('a');
		queue.add(rom('b'));
		queue.clearCompleted();
		expect(queue.items.map((item) => item.romId)).toEqual(['b']);
	});

	it('survives a reload on the same storage, in the format it always used', () => {
		const storage = memoryStorage();
		new DownloadQueue(vi.fn(), storage).add(rom('a'));
		expect(JSON.parse(storage.values.get(DOWNLOAD_QUEUE_STORAGE_KEY)!)[0]).toMatchObject({ romId: 'a', title: 'Game a', platform: 'SNES', status: 'ready' });
		expect(new DownloadQueue(vi.fn(), storage).items.map((item) => item.romId)).toEqual(['a']);
	});

	it('treats an item left downloading by a reload as ready again', () => {
		const storage = memoryStorage();
		storage.setItem(DOWNLOAD_QUEUE_STORAGE_KEY, JSON.stringify([{ romId: 'a', title: 'A', platform: 'SNES', status: 'downloading', filename: null, error: null }]));
		expect(new DownloadQueue(vi.fn(), storage).items[0].status).toBe('ready');
	});
});
