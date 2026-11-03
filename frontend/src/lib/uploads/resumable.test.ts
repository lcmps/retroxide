import { describe, expect, it, vi } from 'vitest';
import { ApiClientError } from '#lib/api/errors.js';
import { PART_BYTES, RESUMABLE_FROM_BYTES, uploadResumable, type ResumableApi } from './resumable.js';

const MiB = 1024 * 1024;

/** A fake server holding one session per file, which can drop parts. */
function fakeServer(size: number) {
	let received = 0;
	let sessions = 0;
	const parts: number[] = [];
	const failures: Array<() => never> = [];
	const api: ResumableApi = {
		createSession: vi.fn(async () => {
			sessions++;
			received = 0;
			return { session_id: `ups_${sessions}`, size, received, max_chunk_size: 64 * MiB };
		}),
		getSession: vi.fn(async (sessionId: string) => ({ session_id: sessionId, size, received, max_chunk_size: 64 * MiB })),
		sendPart: vi.fn(async (sessionId: string, offset: number, part: Blob, onProgress: (loaded: number) => void) => {
			const fail = failures.shift();
			if (fail) fail();
			if (offset !== received) throw new ApiClientError({ message: 'offset', code: 'conflict', status: 409 });
			parts.push(offset);
			onProgress(part.size);
			received += part.size;
			const done = received === size;
			return { session_id: sessionId, size, received, max_chunk_size: 64 * MiB, upload: done ? { upload_id: 'upl_1', status: 'processing' as const } : undefined };
		}),
		uploadWhole: vi.fn(async () => ({ upload_id: 'upl_small', status: 'processing' as const }))
	};
	return {
		api,
		parts,
		failNext: (error: unknown) => failures.push(() => { throw error; }),
		setReceived: (bytes: number) => { received = bytes; }
	};
}

function memoryStorage(initial: Record<string, string> = {}) {
	const items = new Map(Object.entries(initial));
	return {
		items,
		getItem: (key: string) => items.get(key) ?? null,
		setItem: (key: string, value: string) => void items.set(key, value),
		removeItem: (key: string) => void items.delete(key)
	};
}

const instant = { sleep: async () => {}, waitUntilOnline: async () => {} };

describe('resumable uploads', () => {
	it('sends small files in one request', async () => {
		const server = fakeServer(10);
		const file = new File(['tiny'], 'tiny.sfc');
		await expect(uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage() })).resolves.toEqual({ upload_id: 'upl_small', status: 'processing' });
		expect(server.api.createSession).not.toHaveBeenCalled();
	});

	it('sends large files in parts and retries a dropped part where it stopped', async () => {
		const size = RESUMABLE_FROM_BYTES + 3 * MiB;
		const server = fakeServer(size);
		const storage = memoryStorage();
		const reconnecting: boolean[] = [];
		const progress: number[] = [];
		server.failNext(new ApiClientError({ message: 'offline' }));
		const file = new File([new Uint8Array(size)], 'big.iso', { lastModified: 1 });
		const accepted = await uploadResumable(file, server.api, {
			onProgress: (loaded) => progress.push(loaded),
			onReconnecting: (value) => reconnecting.push(value)
		}, { ...instant, storage });
		expect(accepted.upload_id).toBe('upl_1');
		expect(server.parts).toEqual([0, PART_BYTES, 2 * PART_BYTES]);
		expect(reconnecting).toContain(true);
		expect(reconnecting.at(-1)).toBe(false);
		expect(progress.at(-1)).toBe(size);
		expect(storage.items.size).toBe(0); // a finished upload is forgotten
	});

	it('resumes a session remembered from an earlier page load', async () => {
		const size = RESUMABLE_FROM_BYTES + MiB;
		const server = fakeServer(size);
		server.setReceived(2 * PART_BYTES);
		const file = new File([new Uint8Array(size)], 'big.iso', { lastModified: 7 });
		const storage = memoryStorage({ [`retroxide.upload-session:big.iso:${size}:7`]: 'ups_earlier' });
		await uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage });
		expect(server.api.createSession).not.toHaveBeenCalled();
		expect(server.parts).toEqual([2 * PART_BYTES]);
	});

	it('starts over when the session expired, and stops on final errors', async () => {
		const size = RESUMABLE_FROM_BYTES;
		const server = fakeServer(size);
		server.failNext(new ApiClientError({ message: 'gone', code: 'not_found', status: 404 }));
		const file = new File([new Uint8Array(size)], 'big.iso');
		await uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage() });
		expect(server.api.createSession).toHaveBeenCalledTimes(2);

		// A duplicate is final, even though it is a 409 too.
		const duplicate = fakeServer(size);
		const storage = memoryStorage();
		duplicate.failNext(new ApiClientError({ message: 'exists', code: 'rom_already_exists', status: 409 }));
		await expect(uploadResumable(file, duplicate.api, { onProgress: () => {} }, { ...instant, storage })).rejects.toMatchObject({ code: 'rom_already_exists' });
		expect(duplicate.api.sendPart).toHaveBeenCalledTimes(1);
		expect(storage.items.size).toBe(0);
	});

	it('gives up after repeated network failures', async () => {
		const size = RESUMABLE_FROM_BYTES;
		const server = fakeServer(size);
		for (let attempt = 0; attempt < 30; attempt++) server.failNext(new ApiClientError({ message: 'offline' }));
		const file = new File([new Uint8Array(size)], 'big.iso');
		await expect(uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage() })).rejects.toThrow('offline');
	});
});
