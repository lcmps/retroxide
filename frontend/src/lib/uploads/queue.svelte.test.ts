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
		queue.stop();
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
		queue.stop();
	});

	it('skips queued transfers once stopped', async () => {
		const { send, pending } = controllableSend();
		const queue = new UploadQueue(send, async (id) => completed(id), { concurrency: 1 });

		queue.add([rom('a.nes'), rom('b.nes')]);
		await flush();
		queue.stop();
		pending[0].resolve({ upload_id: 'upl_a', status: 'processing' });
		await flush();

		expect(send).toHaveBeenCalledTimes(1);
		expect(queue.items[1].phase).toBe('queued');
	});
});
