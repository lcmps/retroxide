import { createUploadSession, getUploadSession, sendUploadPart, uploadRom } from '#lib/api/client.js';
import { ApiClientError } from '#lib/api/errors.js';
import type { UploadAcceptedResponse } from '#lib/api/types.js';

/** Files from this size are sent in parts that survive a dropped connection. */
export const RESUMABLE_FROM_BYTES = 16 * 1024 * 1024;
/** How much of a file each request carries. */
export const PART_BYTES = 8 * 1024 * 1024;
/** Waits before retrying a failed part, in milliseconds; the last repeats. */
const RETRY_DELAYS_MS = [1_000, 2_000, 4_000, 8_000, 15_000, 30_000];
/** Consecutive failures of one part before giving up, about ten minutes in all. */
const MAX_ATTEMPTS = 24;

const STORAGE_PREFIX = 'retroxide.upload-session:';

type Session = { session_id: string; size: number; received: number; max_chunk_size: number; upload?: UploadAcceptedResponse };

/** The server calls a resumable upload needs, injectable for tests. */
export type ResumableApi = {
	createSession(file: File): Promise<Session>;
	getSession(sessionId: string): Promise<Session>;
	sendPart(sessionId: string, offset: number, part: Blob, onProgress: (loadedBytes: number) => void): Promise<Session>;
	/** Uploads a small file in one request. */
	uploadWhole(file: File, onProgress: (loadedBytes: number, totalBytes: number) => void): Promise<UploadAcceptedResponse>;
};

export type ResumableEnvironment = {
	storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null;
	sleep?: (milliseconds: number) => Promise<void>;
	/** Resolves once the browser is online again. */
	waitUntilOnline?: () => Promise<void>;
};

export type ResumableCallbacks = {
	onProgress: (loadedBytes: number, totalBytes: number) => void;
	/** Reports whether the upload is waiting to retry after a failure. */
	onReconnecting?: (reconnecting: boolean) => void;
};

/** Identifies a file across page loads, so adding it again resumes its upload. */
function fingerprint(file: File): string {
	return `${STORAGE_PREFIX}${file.name}:${file.size}:${file.lastModified}`;
}

function defaultStorage(): ResumableEnvironment['storage'] {
	try {
		return globalThis.localStorage ?? null;
	} catch {
		return null;
	}
}

const defaultSleep = (milliseconds: number) => new Promise<void>((resolve) => setTimeout(resolve, milliseconds));

function defaultWaitUntilOnline(): Promise<void> {
	if (typeof navigator === 'undefined' || navigator.onLine !== false) return Promise.resolve();
	return new Promise((resolve) => window.addEventListener('online', () => resolve(), { once: true }));
}

/** Whether a failure is worth retrying: the network, a timeout, or a server that is struggling. */
function isTransient(error: unknown): boolean {
	if (!(error instanceof ApiClientError)) return false;
	const status = error.status;
	return status === undefined || status === 0 || status === 408 || status === 429 || status >= 500;
}

function isStatus(error: unknown, status: number): boolean {
	return error instanceof ApiClientError && error.status === status;
}

/**
 * Uploads a file. Large files go in parts: a dropped connection retries the part with growing
 * waits (and waits for the browser to be online), and adding the same file again later, even
 * after reloading the page, continues where it stopped.
 */
export async function uploadResumable(
	file: File,
	api: ResumableApi,
	{ onProgress, onReconnecting }: ResumableCallbacks,
	{ storage = defaultStorage(), sleep = defaultSleep, waitUntilOnline = defaultWaitUntilOnline }: ResumableEnvironment = {}
): Promise<UploadAcceptedResponse> {
	if (file.size < RESUMABLE_FROM_BYTES) {
		return api.uploadWhole(file, onProgress);
	}
	const key = fingerprint(file);
	const remember = (sessionId: string | null) => {
		try {
			if (sessionId) storage?.setItem(key, sessionId);
			else storage?.removeItem(key);
		} catch {
			// Without storage the upload still works; it just cannot resume after a reload.
		}
	};

	let session = await resumeOrCreate(file, api, storage?.getItem(key) ?? null);
	remember(session.session_id);
	onProgress(session.received, file.size);

	let failures = 0;
	while (!session.upload) {
		const partSize = Math.min(PART_BYTES, session.max_chunk_size || PART_BYTES);
		const offset = session.received;
		const part = file.slice(offset, Math.min(offset + partSize, file.size));
		try {
			session = await api.sendPart(session.session_id, offset, part, (loaded) => onProgress(offset + loaded, file.size));
			failures = 0;
			onReconnecting?.(false);
			onProgress(session.received, file.size);
		} catch (error) {
			if (isStatus(error, 404)) {
				// The session expired or was removed: start over.
				session = await api.createSession(file);
				remember(session.session_id);
				continue;
			}
			if (isStatus(error, 409) && error instanceof ApiClientError && error.code === 'conflict') {
				// Another request moved the session on (or a part was received twice). Other
				// conflicts, such as the ROM already being in the library, are final.
				session = await api.getSession(session.session_id);
				continue;
			}
			if (!isTransient(error) || ++failures >= MAX_ATTEMPTS) {
				onReconnecting?.(false);
				if (!isTransient(error)) remember(null);
				throw error;
			}
			onReconnecting?.(true);
			await sleep(RETRY_DELAYS_MS[Math.min(failures - 1, RETRY_DELAYS_MS.length - 1)]);
			await waitUntilOnline();
			// The part may have arrived before the connection dropped; ask where to continue.
			try {
				session = await api.getSession(session.session_id);
			} catch (statusError) {
				if (isStatus(statusError, 404)) {
					session = await api.createSession(file);
					remember(session.session_id);
				}
			}
		}
	}
	remember(null);
	onReconnecting?.(false);
	onProgress(file.size, file.size);
	return session.upload;
}

async function resumeOrCreate(file: File, api: ResumableApi, sessionId: string | null): Promise<Session> {
	if (sessionId) {
		try {
			const session = await api.getSession(sessionId);
			if (session.size === file.size) return session;
		} catch (error) {
			if (!isStatus(error, 404) && !isStatus(error, 400)) throw error;
		}
	}
	return api.createSession(file);
}

/** The server API, as the upload page uses it. */
export const serverApi: ResumableApi = {
	createSession: createUploadSession,
	getSession: getUploadSession,
	sendPart: sendUploadPart,
	uploadWhole: uploadRom
};
