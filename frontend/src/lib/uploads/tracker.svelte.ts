import type { UploadState, UploadStatusResponse } from '#lib/api/types.js';

export type FetchUploadStatus = (uploadId: string) => Promise<UploadStatusResponse>;

export type UploadTrackerOptions = {
	/** Delay between successful status checks. */
	intervalMs?: number;
	/** Ceiling for the exponential backoff applied after failed checks. */
	maxDelayMs?: number;
};

export function isTerminalUploadState(state: UploadState): boolean {
	return state === 'completed' || state === 'failed';
}

/**
 * Polls an accepted upload until processing completes or fails, exposing the latest status
 * reactively. Failed checks are retried with exponential backoff; superseded polls are ignored.
 */
export class UploadTracker {
	/** Latest known status, or null when nothing is being tracked. */
	status = $state<UploadStatusResponse | null>(null);
	/** Whether the most recent status check failed (a retry is scheduled). */
	failing = $state(false);

	readonly #fetchStatus: FetchUploadStatus;
	readonly #intervalMs: number;
	readonly #maxDelayMs: number;
	#timer: ReturnType<typeof setTimeout> | undefined;
	#generation = 0;
	#failures = 0;

	constructor(fetchStatus: FetchUploadStatus, { intervalMs = 2_000, maxDelayMs = 30_000 }: UploadTrackerOptions = {}) {
		this.#fetchStatus = fetchStatus;
		this.#intervalMs = intervalMs;
		this.#maxDelayMs = maxDelayMs;
	}

	/** Starts tracking from an upload's initial status, polling immediately unless it is already final. */
	track(initial: UploadStatusResponse): void {
		this.stop();
		this.status = initial;
		if (!isTerminalUploadState(initial.status)) {
			this.#schedule(this.#generation, 0);
		}
	}

	/** Stops polling and keeps the last known status. */
	stop(): void {
		this.#generation += 1;
		this.#failures = 0;
		this.failing = false;
		clearTimeout(this.#timer);
		this.#timer = undefined;
	}

	/** Stops polling and forgets the tracked upload. */
	reset(): void {
		this.stop();
		this.status = null;
	}

	#schedule(generation: number, delayMs: number): void {
		this.#timer = setTimeout(() => void this.#poll(generation), delayMs);
	}

	async #poll(generation: number): Promise<void> {
		const uploadId = this.status?.upload_id;
		if (!uploadId) {
			return;
		}
		try {
			const next = await this.#fetchStatus(uploadId);
			if (generation !== this.#generation) {
				return;
			}
			this.status = next;
			this.#failures = 0;
			this.failing = false;
			if (!isTerminalUploadState(next.status)) {
				this.#schedule(generation, this.#intervalMs);
			}
		} catch {
			if (generation !== this.#generation) {
				return;
			}
			this.#failures += 1;
			this.failing = true;
			this.#schedule(generation, Math.min(this.#intervalMs * 2 ** (this.#failures - 1), this.#maxDelayMs));
		}
	}
}
