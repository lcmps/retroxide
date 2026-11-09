import { afterEach, describe, expect, it, vi } from 'vitest';
import { hashFileInBackground } from './background-hash.js';
import { hashFile } from './hash.js';

/** A stand-in Worker; each test decides how it answers the posted file. */
class FakeWorker {
	static instances: FakeWorker[] = [];
	onmessage: ((event: { data: unknown }) => void) | null = null;
	onerror: ((event: { message: string; preventDefault(): void }) => void) | null = null;
	posted: unknown[] = [];
	terminated = false;
	constructor(readonly url: URL, readonly options: WorkerOptions) {
		FakeWorker.instances.push(this);
	}
	postMessage(message: unknown) {
		this.posted.push(message);
	}
	terminate() {
		this.terminated = true;
	}
	answer(data: unknown) {
		this.onmessage?.({ data });
	}
	fail() {
		this.onerror?.({ message: 'failed to load', preventDefault() {} });
	}
}

const content = new Blob([new Uint8Array(1024).map((_, index) => index & 0xff)]);

function useFakeWorker(): () => FakeWorker {
	FakeWorker.instances = [];
	vi.stubGlobal('Worker', FakeWorker);
	return () => FakeWorker.instances.at(-1) as FakeWorker;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('hashFileInBackground', () => {
	it('hashes on the main thread where workers are unavailable', async () => {
		await expect(hashFileInBackground(content)).resolves.toBe(await hashFile(content));
	});

	it('hashes in a module worker and ends it with the answer', async () => {
		const worker = useFakeWorker();
		const hashing = hashFileInBackground(content);
		expect(worker().options).toEqual({ type: 'module' });
		expect(worker().posted).toEqual([content]);
		worker().answer({ sha256: 'f'.repeat(64) });
		await expect(hashing).resolves.toBe('f'.repeat(64));
		expect(worker().terminated).toBe(true);
	});

	it('reports a failure the worker sends back', async () => {
		const worker = useFakeWorker();
		const hashing = hashFileInBackground(content);
		worker().answer({ error: 'unreadable file' });
		await expect(hashing).rejects.toThrow('unreadable file');
		expect(worker().terminated).toBe(true);
	});

	it('falls back to the main thread when the worker cannot start', async () => {
		const worker = useFakeWorker();
		const hashing = hashFileInBackground(content);
		worker().fail();
		await expect(hashing).resolves.toBe(await hashFile(content));
		expect(worker().terminated).toBe(true);
	});

	it('ends the worker when aborted', async () => {
		const worker = useFakeWorker();
		const controller = new AbortController();
		const hashing = hashFileInBackground(content, controller.signal);
		controller.abort();
		await expect(hashing).rejects.toMatchObject({ name: 'AbortError' });
		expect(worker().terminated).toBe(true);
	});

	it('does not start a worker when already aborted', async () => {
		useFakeWorker();
		await expect(hashFileInBackground(content, AbortSignal.abort())).rejects.toMatchObject({ name: 'AbortError' });
		expect(FakeWorker.instances).toHaveLength(0);
	});
});
