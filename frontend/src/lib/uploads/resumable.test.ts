import { describe, expect, it, vi } from 'vitest';
import { ApiClientError } from '#lib/api/errors.js';
import { MIN_PART_BYTES, PART_SECONDS, RESUMABLE_FROM_BYTES, uploadResumable, type ResumableApi } from './resumable.js';

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
		uploadWhole: vi.fn(async () => ({ upload_id: 'upl_small', status: 'processing' as const })),
		checkDuplicate: vi.fn(async () => {})
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

// Hashing never finishes unless a test says so, so other tests see no duplicate checks.
const never = () => new Promise<string>(() => {});
// A clock that never moves, so parts stay at their starting size unless a test sets a pace.
const instant = { sleep: async () => {}, waitUntilOnline: async () => {}, hash: never, now: () => 0 };

describe('resumable uploads', () => {
	it('sends small files in one request', async () => {
		const server = fakeServer(10);
		const file = new File(['tiny'], 'tiny.sfc');
		await expect(uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage() })).resolves.toEqual({ upload_id: 'upl_small', status: 'processing' });
		expect(server.api.createSession).not.toHaveBeenCalled();
	});

	it('sends large files in parts and retries a dropped part where it stopped', async () => {
		const size = 2 * MIN_PART_BYTES + 3 * MiB;
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
		expect(server.parts).toEqual([0, MIN_PART_BYTES, 2 * MIN_PART_BYTES]);
		expect(reconnecting).toContain(true);
		expect(reconnecting.at(-1)).toBe(false);
		expect(progress.at(-1)).toBe(size);
		expect(storage.items.size).toBe(0); // a finished upload is forgotten
	});

	it('resumes a session remembered from an earlier page load', async () => {
		const size = 2 * MIN_PART_BYTES + MiB;
		const server = fakeServer(size);
		server.setReceived(2 * MIN_PART_BYTES);
		const file = new File([new Uint8Array(size)], 'big.iso', { lastModified: 7 });
		const storage = memoryStorage({ [`retroxide.upload-session:big.iso:${size}:7`]: 'ups_earlier' });
		await uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage });
		expect(server.api.createSession).not.toHaveBeenCalled();
		expect(server.parts).toEqual([2 * MIN_PART_BYTES]);
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

describe('duplicate checks while uploading', () => {
	it('stops uploading a ROM the library already has once the file is hashed', async () => {
		const size = 2 * MIN_PART_BYTES + 8 * MiB;
		const server = fakeServer(size);
		let finishHash: (digest: string) => void = () => {};
		const hashed = new Promise<string>((resolve) => (finishHash = resolve));
		server.api.checkDuplicate = vi.fn(async () => {
			throw new ApiClientError({ message: 'exists', code: 'rom_already_exists', status: 409 });
		});
		const sendPart = server.api.sendPart;
		server.api.sendPart = vi.fn(async (...args: Parameters<typeof sendPart>) => {
			const result = await sendPart(...args);
			finishHash('a'.repeat(64)); // hashing finishes during the first part
			return result;
		});
		const storage = memoryStorage();
		const file = new File([new Uint8Array(size)], 'known.iso');
		await expect(uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage, hash: () => hashed })).rejects.toMatchObject({
			code: 'rom_already_exists'
		});
		expect(server.api.checkDuplicate).toHaveBeenCalledWith('ups_1', 'a'.repeat(64));
		expect(server.parts).toEqual([0]); // one part of three
		expect(storage.items.size).toBe(0);
	});

	it('uploads new ROMs in full, checks once, and stops hashing when done', async () => {
		const size = 2 * MIN_PART_BYTES + 8 * MiB;
		const server = fakeServer(size);
		let aborted = false;
		const hash = (_file: File, signal: AbortSignal) => {
			signal.addEventListener('abort', () => (aborted = true));
			return Promise.resolve('b'.repeat(64));
		};
		const file = new File([new Uint8Array(size)], 'new.iso');
		await uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage(), hash });
		expect(server.parts).toHaveLength(3);
		expect(server.api.checkDuplicate).toHaveBeenCalledTimes(1);
		expect(aborted).toBe(true);
	});

	it('goes on when the check itself fails', async () => {
		const size = RESUMABLE_FROM_BYTES;
		const server = fakeServer(size);
		server.api.checkDuplicate = vi.fn(async () => {
			throw new ApiClientError({ message: 'offline' });
		});
		const file = new File([new Uint8Array(size)], 'new.iso');
		const accepted = await uploadResumable(file, server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage(), hash: async () => 'c'.repeat(64) });
		expect(accepted.upload_id).toBe('upl_1');
	});

	describe('cancelling', () => {
		// Large enough to be sent in parts, and to need more than one.
		const large = () => new File([new Uint8Array(Math.max(RESUMABLE_FROM_BYTES, MIN_PART_BYTES + MiB))], 'big.bin');
		const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
		const noHash = async () => '';

		it('passes the signal to a small upload', async () => {
			const server = fakeServer(4);
			const { signal } = new AbortController();
			const onProgress = vi.fn();
			await uploadResumable(new File(['rom!'], 'a.nes'), server.api, { onProgress, signal });
			expect(server.api.uploadWhole).toHaveBeenCalledWith(expect.any(File), onProgress, signal);
		});

		it('stops at the part in flight, keeps the session to resume, and does not retry', async () => {
			const file = large();
			const server = fakeServer(file.size);
			const controller = new AbortController();
			vi.mocked(server.api.sendPart).mockImplementationOnce(
				(_session, _offset, _part, _progress, signal) =>
					new Promise((_resolve, reject) => signal?.addEventListener('abort', () => reject(signal.reason)))
			);
			const storage = memoryStorage();
			const sleep = vi.fn(async () => {});
			const upload = uploadResumable(file, server.api, { onProgress: vi.fn(), signal: controller.signal }, { storage, sleep, hash: noHash });
			await flush();
			controller.abort();

			await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
			expect(server.api.sendPart).toHaveBeenCalledTimes(1);
			expect(sleep).not.toHaveBeenCalled();
			expect([...storage.items.values()]).toEqual(['ups_1']);
		});

		it('sends nothing more once aborted between parts', async () => {
			const file = large();
			const server = fakeServer(file.size);
			const controller = new AbortController();
			const send = server.api.sendPart;
			server.api.sendPart = vi.fn(async (...args: Parameters<ResumableApi['sendPart']>) => {
				const session = await send(...args);
				controller.abort();
				return session;
			});
			const upload = uploadResumable(file, server.api, { onProgress: vi.fn(), signal: controller.signal }, { storage: memoryStorage(), hash: noHash });

			await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
			expect(server.api.sendPart).toHaveBeenCalledTimes(1);
		});

		it('wakes from waiting to retry', async () => {
			const file = large();
			const server = fakeServer(file.size);
			server.failNext(new ApiClientError({ message: 'offline' }));
			const controller = new AbortController();
			const upload = uploadResumable(file, server.api, { onProgress: vi.fn(), signal: controller.signal }, { storage: memoryStorage(), sleep: () => new Promise(() => {}), hash: noHash });
			await flush();
			controller.abort();
			await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
		});

		it('wakes from waiting to be online', async () => {
			const file = large();
			const server = fakeServer(file.size);
			server.failNext(new ApiClientError({ message: 'offline' }));
			const controller = new AbortController();
			const upload = uploadResumable(file, server.api, { onProgress: vi.fn(), signal: controller.signal }, { storage: memoryStorage(), sleep: async () => {}, waitUntilOnline: () => new Promise(() => {}), hash: noHash });
			await flush();
			controller.abort();
			await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
		});

		it('does not start when already aborted', async () => {
			const file = large();
			const server = fakeServer(file.size);
			const upload = uploadResumable(file, server.api, { onProgress: vi.fn(), signal: AbortSignal.abort() }, { storage: memoryStorage(), hash: noHash });
			await expect(upload).rejects.toMatchObject({ name: 'AbortError' });
			expect(server.api.sendPart).not.toHaveBeenCalled();
		});
	});
});

describe('part sizes', () => {
	/** Delivers every part at bytesPerSecond, moving the returned clock on as it does. */
	function atSpeed(server: ReturnType<typeof fakeServer>, bytesPerSecond: number): () => number {
		let milliseconds = 0;
		const send = server.api.sendPart;
		server.api.sendPart = vi.fn(async (...args: Parameters<ResumableApi['sendPart']>) => {
			milliseconds += (args[2].size / bytesPerSecond) * 1000;
			return send(...args);
		});
		return () => milliseconds;
	}

	/** The size of each part the server received, in MiB. */
	function partSizes(server: ReturnType<typeof fakeServer>, size: number): number[] {
		return server.parts.map((offset, index) => ((server.parts[index + 1] ?? size) - offset) / MiB);
	}

	const upload = (server: ReturnType<typeof fakeServer>, size: number, now: () => number) =>
		uploadResumable(new File([new Uint8Array(size)], 'paced.iso'), server.api, { onProgress: () => {} }, { ...instant, storage: memoryStorage(), now });

	it('grows parts on a fast link up to the largest the server accepts', async () => {
		const size = (8 + 64 + 64 + 5) * MiB;
		const server = fakeServer(size);
		await upload(server, size, atSpeed(server, 1024 * MiB));
		expect(partSizes(server, size)).toEqual([8, 64, 64, 5]);
	});

	it('sizes parts to take about PART_SECONDS each', async () => {
		const size = (8 + 16 + 16 + 3) * MiB;
		const server = fakeServer(size);
		await upload(server, size, atSpeed(server, (16 * MiB) / PART_SECONDS));
		expect(partSizes(server, size)).toEqual([8, 16, 16, 3]);
	});

	it('keeps parts at the smallest size on a slow link', async () => {
		const size = 3 * MIN_PART_BYTES;
		const server = fakeServer(size);
		await upload(server, size, atSpeed(server, MiB / 8));
		expect(partSizes(server, size)).toEqual([8, 8, 8]);
	});

	it('halves the part after a failure, then grows again', async () => {
		const size = (8 + 16 + 32 + 4) * MiB;
		const server = fakeServer(size);
		const now = atSpeed(server, (32 * MiB) / PART_SECONDS);
		const paced = server.api.sendPart;
		let calls = 0;
		server.api.sendPart = vi.fn(async (...args: Parameters<ResumableApi['sendPart']>) => {
			if (++calls === 2) server.failNext(new ApiClientError({ message: 'offline' }));
			return paced(...args);
		});
		await upload(server, size, now);
		expect(partSizes(server, size)).toEqual([8, 16, 32, 4]);
	});
});
