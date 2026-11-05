import { describe, expect, it, vi } from 'vitest';
import type { UploadAcceptedResponse, UploadStatusResponse } from '#lib/api/types.js';
import { UploadQueue, type SendUpload } from './queue.svelte.js';

type PendingSend = {
	file: File;
	progress: (loaded: number, total: number) => void;
	resolve: (accepted: UploadAcceptedResponse) => void;
	reject: (error: unknown) => void;
};

/** A send function whose transfers stay in flight until the test settles them. */
function controllableSend() {
	const pending: PendingSend[] = [];
	const send = vi.fn<SendUpload>(
		(file, progress) => new Promise((resolve, reject) => pending.push({ file, progress, resolve, reject }))
	);
	return { send, pending };
}

const rom = (name: string) => new File(['rom'], name);
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const completed = (uploadId: string): UploadStatusResponse => ({
	upload_id: uploadId,
	status: 'completed',
	rom_id: `rom_${uploadId}`,
	error: null
});

describe('UploadQueue', () => {
	it('transfers files in order with bounded concurrency', async () => {
		const { send, pending } = controllableSend();
		const queue = new UploadQueue(send, async (id) => completed(id), { concurrency: 2 });

		queue.add([rom('a.nes'), rom('b.nes'), rom('c.nes')]);
		await flush();

		expect(send.mock.calls.map(([file]) => file.name)).toEqual(['a.nes', 'b.nes']);
		expect(queue.items.map((item) => item.phase)).toEqual(['uploading', 'uploading', 'queued']);
		expect(queue.transferring).toBe(true);

		pending[0].progress(1, 4);
		expect(queue.items[0].progress).toBe(25);

		pending[0].resolve({ upload_id: 'upl_a', status: 'processing' });
		await flush();
		expect(send.mock.calls.map(([file]) => file.name)).toEqual(['a.nes', 'b.nes', 'c.nes']);
		expect(queue.items[0].phase).toBe('accepted');
		expect(queue.items[0].progress).toBe(100);

		pending[1].resolve({ upload_id: 'upl_b', status: 'processing' });
		pending[2].resolve({ upload_id: 'upl_c', status: 'processing' });
		await flush();
		expect(queue.transferring).toBe(false);
		queue.cancel();
	});

	it('tracks each accepted upload independently and clears finished items', async () => {
		const { send, pending } = controllableSend();
		const fetchStatus = vi.fn(async (id: string) => completed(id));
		const queue = new UploadQueue(send, fetchStatus, { concurrency: 2 });

		queue.add([rom('a.nes'), rom('b.nes')]);
		await flush();
		pending[0].resolve({ upload_id: 'upl_a', status: 'processing' });
		pending[1].reject(new Error('too large'));
		await flush();
		await flush();

		const [accepted, rejected] = queue.items;
		expect(fetchStatus).toHaveBeenCalledWith('upl_a');
		expect(accepted.tracker.status).toEqual(completed('upl_a'));
		expect(accepted.finished).toBe(true);
		expect(rejected.phase).toBe('rejected');
		expect(rejected.error).toBeInstanceOf(Error);

		queue.add([rom('c.nes')]);
		await flush();
		queue.clearFinished();
		expect(queue.items.map((item) => item.file.name)).toEqual(['c.nes']);
		queue.cancel();
	});

	it('counts the files still to transfer', async () => {
		const { send, pending } = controllableSend();
		const queue = new UploadQueue(send, async (id) => completed(id), { concurrency: 1 });

		queue.add([rom('a.nes'), rom('b.nes'), rom('c.nes')]);
		await flush();
		expect(queue.transferringCount).toBe(3);

		pending[0].reject(new Error('too large'));
		await flush();
		expect(queue.transferringCount).toBe(2);

		pending[1].resolve({ upload_id: 'upl_b', status: 'processing' });
		await flush();
		expect(queue.transferringCount).toBe(1);
		queue.cancel();
	});

	it('cancelling aborts what is running, drops what is waiting, and empties the list', async () => {
		const { send, pending } = controllableSend();
		const fetchStatus = vi.fn(async (id: string) => completed(id));
		const queue = new UploadQueue(send, fetchStatus, { concurrency: 1 });

		queue.add([rom('a.nes'), rom('b.nes')]);
		await flush();
		const signal = send.mock.calls[0][3] as AbortSignal;
		expect(signal.aborted).toBe(false);

		queue.cancel();
		expect(signal.aborted).toBe(true);
		expect(queue.items).toEqual([]);
		expect(queue.transferring).toBe(false);

		// A transfer accepted just as it was cancelled is not tracked or brought back.
		pending[0].resolve({ upload_id: 'upl_a', status: 'processing' });
		await flush();
		expect(send).toHaveBeenCalledTimes(1);
		expect(fetchStatus).not.toHaveBeenCalled();
		expect(queue.items).toEqual([]);
	});

	it('stops following uploads being processed when cancelled', async () => {
		vi.useFakeTimers();
		try {
			const { send, pending } = controllableSend();
			const fetchStatus = vi.fn(async (id: string): Promise<UploadStatusResponse> => ({ upload_id: id, status: 'processing', rom_id: null, error: null }));
			const queue = new UploadQueue(send, fetchStatus, { concurrency: 1 });
			queue.add([rom('a.nes')]);
			await vi.advanceTimersByTimeAsync(0);
			pending[0].resolve({ upload_id: 'upl_a', status: 'processing' });
			await vi.advanceTimersByTimeAsync(0);
			const calls = fetchStatus.mock.calls.length;
			expect(calls).toBeGreaterThan(0);

			queue.cancel();
			await vi.advanceTimersByTimeAsync(600_000);
			expect(fetchStatus).toHaveBeenCalledTimes(calls);
		} finally {
			vi.useRealTimers();
		}
	});

	it('works again after cancelling, without waiting on the cancelled transfers', async () => {
		const send = vi.fn<SendUpload>(
			(_file, _progress, _reconnecting, signal) =>
				new Promise((resolve, reject) => {
					if (_file.name === 'stuck.nes') signal?.addEventListener('abort', () => reject(signal.reason));
					else resolve({ upload_id: 'upl_new', status: 'processing' });
				})
		);
		const queue = new UploadQueue(send, async (id) => completed(id), { concurrency: 1 });
		queue.add([rom('stuck.nes')]);
		await flush();
		queue.cancel();
		await flush();

		queue.add([rom('new.nes')]);
		await flush();
		await flush();
		expect(queue.items.map((item) => [item.file.name, item.phase])).toEqual([['new.nes', 'accepted']]);
		expect((send.mock.calls[1][3] as AbortSignal).aborted).toBe(false);
		queue.cancel();
	});
});
