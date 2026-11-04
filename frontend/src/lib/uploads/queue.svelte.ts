import type { UploadAcceptedResponse } from '#lib/api/types.js';
import type { DiscSelection } from './disc.js';
import { createLimiter } from '#lib/helpers/concurrency.js';
import { isTerminalUploadState, UploadTracker, type FetchUploadStatus, type UploadTrackerOptions } from './tracker.svelte.js';

export type SendUpload = (
	file: File,
	onProgress: (loadedBytes: number, totalBytes: number) => void,
	/** Reports whether the transfer is waiting to retry after a dropped connection. */
	onReconnecting?: (reconnecting: boolean) => void
) => Promise<UploadAcceptedResponse>;

/** queued → uploading → accepted (processing is then tracked) or rejected (the transfer failed). */
export type UploadPhase = 'queued' | 'uploading' | 'accepted' | 'rejected';

/** Sends a disc: its track files, then its sheet. */
export type SendDisc = (
	disc: DiscSelection,
	onProgress: (loadedBytes: number, totalBytes: number) => void,
	onReconnecting?: (reconnecting: boolean) => void
) => Promise<UploadAcceptedResponse>;

export type UploadQueueOptions = {
	/** Maximum number of files transferred at once. */
	concurrency?: number;
	tracker?: UploadTrackerOptions;
	sendDisc?: SendDisc;
};

/** One file in the queue: its transfer progress and, once accepted, its processing status. */
export class UploadItem {
	readonly key: number;
	readonly file: File;
	phase = $state<UploadPhase>('queued');
	/** Transfer progress as a percentage from 0 to 100. */
	progress = $state(0);
	/** Whether the transfer lost its connection and is waiting to retry. */
	reconnecting = $state(false);
	/** Why the transfer was rejected; translated by the view. */
	error = $state.raw<unknown>(null);
	readonly tracker: UploadTracker;
	/** Set for a disc: the file is its sheet, and these are its tracks. */
	readonly disc: DiscSelection | null;

	constructor(key: number, file: File, tracker: UploadTracker, disc: DiscSelection | null = null) {
		this.key = key;
		this.file = file;
		this.tracker = tracker;
		this.disc = disc;
	}

	/** Whether nothing further will happen to this item. */
	get finished(): boolean {
		if (this.phase === 'rejected') {
			return true;
		}
		const status = this.tracker.status;
		return this.phase === 'accepted' && status !== null && isTerminalUploadState(status.status);
	}
}

/**
 * Uploads many files, a few at a time, in the order they were added. Each accepted upload is
 * tracked until processing completes or fails. Files can be added while others are in flight.
 */
export class UploadQueue {
	items = $state<UploadItem[]>([]);

	readonly #send: SendUpload;
	readonly #sendDisc: SendDisc | undefined;
	readonly #fetchStatus: FetchUploadStatus;
	readonly #trackerOptions: UploadTrackerOptions | undefined;
	readonly #limit: ReturnType<typeof createLimiter>;
	#nextKey = 0;
	#stopped = false;

	constructor(send: SendUpload, fetchStatus: FetchUploadStatus, { concurrency = 2, tracker, sendDisc }: UploadQueueOptions = {}) {
		this.#send = send;
		this.#sendDisc = sendDisc;
		this.#fetchStatus = fetchStatus;
		this.#trackerOptions = tracker;
		this.#limit = createLimiter(concurrency);
	}

	/** Whether any file is still waiting for, or in the middle of, its transfer. */
	get transferring(): boolean {
		return this.items.some((item) => item.phase === 'queued' || item.phase === 'uploading');
	}

	/** Queues files for upload and starts transferring them as slots free up. */
	add(files: Iterable<File>): void {
		this.#stopped = false;
		for (const file of files) {
			const item = new UploadItem(this.#nextKey++, file, new UploadTracker(this.#fetchStatus, this.#trackerOptions));
			this.items.push(item);
			void this.#limit(() => this.#transfer(item));
		}
	}

	/**
	 * Queues a disc. One whose sheet names files that were not picked is rejected at once, so
	 * nothing uploads until every track is there.
	 */
	addDisc(disc: DiscSelection): void {
		this.#stopped = false;
		const item = new UploadItem(this.#nextKey++, disc.sheet, new UploadTracker(this.#fetchStatus, this.#trackerOptions), disc);
		this.items.push(item);
		if (disc.missing.length > 0 || !this.#sendDisc) {
			item.phase = 'rejected';
			return;
		}
		void this.#limit(() => this.#transfer(item));
	}

	/** Removes items that have finished, successfully or not. */
	clearFinished(): void {
		this.items = this.items.filter((item) => !item.finished);
	}

	/** Stops polling and skips transfers that have not started; in-flight transfers complete. */
	stop(): void {
		this.#stopped = true;
		for (const item of this.items) {
			item.tracker.stop();
		}
	}

	async #transfer(item: UploadItem): Promise<void> {
		if (this.#stopped || !this.items.includes(item)) {
			return;
		}
		item.phase = 'uploading';
		try {
			const onProgress = (loaded: number, total: number) => {
				item.progress = total > 0 ? Math.round((loaded / total) * 100) : 0;
			};
			const onReconnecting = (reconnecting: boolean) => {
				item.reconnecting = reconnecting;
			};
			const accepted =
				item.disc && this.#sendDisc
					? await this.#sendDisc(item.disc, onProgress, onReconnecting)
					: await this.#send(item.file, onProgress, onReconnecting);
			item.reconnecting = false;
			item.progress = 100;
			item.phase = 'accepted';
			if (!this.#stopped) {
				item.tracker.track({ upload_id: accepted.upload_id, status: accepted.status, rom_id: null, error: null });
			}
		} catch (error) {
			item.error = error;
			item.phase = 'rejected';
		}
	}
}
