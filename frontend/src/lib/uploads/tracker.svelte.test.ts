import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UploadState, UploadStatusResponse } from '#lib/api/types.js';
import { UploadTracker } from './tracker.svelte.js';

const status = (state: UploadState, rom_id: string | null = null): UploadStatusResponse => ({
	upload_id: 'upl_1',
	status: state,
	rom_id,
	error: null
});

describe('UploadTracker', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it('polls until the upload reaches a terminal state', async () => {
		const fetchStatus = vi
			.fn<(id: string) => Promise<UploadStatusResponse>>()
			.mockResolvedValueOnce(status('processing'))
			.mockResolvedValueOnce(status('completed', 'rom_1'));
		const tracker = new UploadTracker(fetchStatus, { intervalMs: 1_000 });

		tracker.track(status('processing'));
		await vi.advanceTimersByTimeAsync(0);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(1_000);
		expect(tracker.status).toEqual(status('completed', 'rom_1'));

		await vi.advanceTimersByTimeAsync(10_000);
		expect(fetchStatus).toHaveBeenCalledTimes(2);
	});

	it('does not poll an upload that is already final', async () => {
		const fetchStatus = vi.fn<(id: string) => Promise<UploadStatusResponse>>();
		new UploadTracker(fetchStatus).track(status('failed'));
		await vi.advanceTimersByTimeAsync(60_000);
		expect(fetchStatus).not.toHaveBeenCalled();
	});

	it('backs off exponentially after failures and recovers', async () => {
		const fetchStatus = vi
			.fn<(id: string) => Promise<UploadStatusResponse>>()
			.mockRejectedValueOnce(new Error('offline'))
			.mockRejectedValueOnce(new Error('offline'))
			.mockResolvedValueOnce(status('completed'));
		const tracker = new UploadTracker(fetchStatus, { intervalMs: 1_000, maxDelayMs: 1_500 });

		tracker.track(status('processing'));
		await vi.advanceTimersByTimeAsync(0);
		expect(tracker.failing).toBe(true);

		await vi.advanceTimersByTimeAsync(999);
		expect(fetchStatus).toHaveBeenCalledTimes(1);
		await vi.advanceTimersByTimeAsync(1);
		expect(fetchStatus).toHaveBeenCalledTimes(2);

		// The second retry would wait 2s but is capped at maxDelayMs.
		await vi.advanceTimersByTimeAsync(1_500);
		expect(fetchStatus).toHaveBeenCalledTimes(3);
		expect(tracker.failing).toBe(false);
		expect(tracker.status?.status).toBe('completed');
	});

	it('ignores responses that arrive after tracking stopped', async () => {
		let resolve: (value: UploadStatusResponse) => void = () => undefined;
		const fetchStatus = vi.fn(() => new Promise<UploadStatusResponse>((done) => (resolve = done)));
		const tracker = new UploadTracker(fetchStatus);

		tracker.track(status('processing'));
		await vi.advanceTimersByTimeAsync(0);
		tracker.reset();
		resolve(status('completed'));
		await vi.advanceTimersByTimeAsync(0);

		expect(tracker.status).toBeNull();
	});
});
