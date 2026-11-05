import { checkUploadSessionDuplicate, createUploadSession, getUploadSession, sendUploadPart, uploadRom } from '#lib/api/client.js';
import { ApiClientError } from '#lib/api/errors.js';
import type { UploadAcceptedResponse } from '#lib/api/types.js';
import { hashFile } from './hash.js';

/** Files from this size are sent in parts that survive a dropped connection. */
export const RESUMABLE_FROM_BYTES = 16 * 1024 * 1024;
/** How much of a file each request carries. */
export const PART_BYTES = 8 * 1024 * 1024;
/** Waits before retrying a failed part, in milliseconds; the last repeats. */
const RETRY_DELAYS_MS = [1_000, 2_000, 4_000, 8_000, 15_000, 30_000];
/** Consecutive failures of one part before giving up, about ten minutes in all. */
const MAX_ATTEMPTS = 24;

const STORAGE_PREFIX = 'retroxide.upload-session:';

type Session = {
	session_id: string;
	size: number;
	received: number;
	max_chunk_size: number;
	upload?: UploadAcceptedResponse;
	part_id?: string;
};

/** The server calls a resumable upload needs, injectable for tests. */
export type ResumableApi = {
	/** Starts a session; with part, for one of a disc's files. */
	createSession(file: File, part?: boolean): Promise<Session>;
	getSession(sessionId: string): Promise<Session>;
	sendPart(sessionId: string, offset: number, part: Blob, onProgress: (loadedBytes: number) => void, signal?: AbortSignal): Promise<Session>;
	/** Uploads a small file in one request. */
	uploadWhole(file: File, onProgress: (loadedBytes: number, totalBytes: number) => void, signal?: AbortSignal): Promise<UploadAcceptedResponse>;
	/** Ends the session with rom_already_exists if the library already has the file. */
	checkDuplicate(sessionId: string, sha256: string): Promise<void>;
};

export type ResumableEnvironment = {
	storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null;
	sleep?: (milliseconds: number) => Promise<void>;
	/** Resolves once the browser is online again. */
	waitUntilOnline?: () => Promise<void>;
	/** Computes a file's SHA-256 as lowercase hex. */
	hash?: (file: File, signal: AbortSignal) => Promise<string>;
};

export type ResumableCallbacks = {
	onProgress: (loadedBytes: number, totalBytes: number) => void;
	/** Reports whether the upload is waiting to retry after a failure. */
	onReconnecting?: (reconnecting: boolean) => void;
	/** Stops the upload, which rejects with the signal's reason; a large file can still resume. */
	signal?: AbortSignal;
};

/** Identifies a file across page loads, so adding it again resumes its upload. */
function fingerprint(file: File, part = false): string {
	return `${STORAGE_PREFIX}${part ? 'part:' : ''}${file.name}:${file.size}:${file.lastModified}`;
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
	callbacks: ResumableCallbacks,
	environment: ResumableEnvironment = {}
): Promise<UploadAcceptedResponse> {
	if (file.size < RESUMABLE_FROM_BYTES) {
		return api.uploadWhole(file, callbacks.onProgress, callbacks.signal);
	}
	const session = await transfer(file, api, false, callbacks, environment);
	return session.upload as UploadAcceptedResponse;
}

/**
 * Uploads one of a disc's files as a part, whatever its size, and returns the part's ID for
 * assembling the disc. Parts resume and retry like large uploads.
 */
export async function uploadPart(
	file: File,
	api: ResumableApi,
	callbacks: ResumableCallbacks,
	environment: ResumableEnvironment = {}
): Promise<string> {
	const session = await transfer(file, api, true, callbacks, environment);
	return session.part_id as string;
}

async function transfer(
	file: File,
	api: ResumableApi,
	part: boolean,
	{ onProgress, onReconnecting, signal }: ResumableCallbacks,
	{ storage = defaultStorage(), sleep = defaultSleep, waitUntilOnline = defaultWaitUntilOnline, hash = hashFile }: ResumableEnvironment
): Promise<Session> {
	const key = fingerprint(file, part);
	const remember = (sessionId: string | null) => {
		try {
			if (sessionId) storage?.setItem(key, sessionId);
			else storage?.removeItem(key);
		} catch {
			// Without storage the upload still works; it just cannot resume after a reload.
		}
	};
	const done = (current: Session) => Boolean(current.upload || current.part_id);
	const create = () => api.createSession(file, part);
	/** Waits for a promise, or until the upload is cancelled. */
	const orCancelled = <T>(waiting: Promise<T>): Promise<T> => {
		if (!signal) return waiting;
		return new Promise<T>((resolve, reject) => {
			const cancel = () => reject(signal.reason);
			if (signal.aborted) cancel();
			signal.addEventListener('abort', cancel, { once: true });
			waiting.then(resolve, reject).finally(() => signal.removeEventListener('abort', cancel));
		});
	};

	signal?.throwIfAborted();
	let session = await resumeOrCreate(file, api, storage?.getItem(key) ?? null, create);
	signal?.throwIfAborted();
	remember(session.session_id);
	onProgress(session.received, file.size);

	// A whole file is hashed while it uploads; hashing is far faster than any home connection, so
	// a ROM the library already has is caught within the first parts instead of after the last.
	// A disc's files are checked as a disc, once assembled.
	const hashing = new AbortController();
	let sha256: string | null = null;
	let checked = part;
	if (!part) {
		hash(file, hashing.signal).then(
			(digest) => (sha256 = digest),
			() => (checked = true)
		);
	}
	const checkForDuplicate = async () => {
		if (checked || !sha256 || done(session)) return;
		checked = true;
		try {
			await api.checkDuplicate(session.session_id, sha256);
		} catch (error) {
			if (error instanceof ApiClientError && error.code === 'rom_already_exists') {
				remember(null);
				throw error;
			}
			// Any other failure just skips the early check; the server still catches duplicates.
		}
	};

	try {
		return await sendParts();
	} finally {
		hashing.abort();
	}

	async function sendParts(): Promise<Session> {
		let failures = 0;
		while (!done(session)) {
			signal?.throwIfAborted();
			await checkForDuplicate();
			const partSize = Math.min(PART_BYTES, session.max_chunk_size || PART_BYTES);
			const offset = session.received;
			const piece = file.slice(offset, Math.min(offset + partSize, file.size));
			try {
				session = await api.sendPart(session.session_id, offset, piece, (loaded) => onProgress(offset + loaded, file.size), signal);
				failures = 0;
				onReconnecting?.(false);
				onProgress(session.received, file.size);
			} catch (error) {
				if (signal?.aborted) throw error;
				if (isStatus(error, 404)) {
					// The session expired or was removed: start over.
					session = await create();
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
				await orCancelled(sleep(RETRY_DELAYS_MS[Math.min(failures - 1, RETRY_DELAYS_MS.length - 1)]));
				await orCancelled(waitUntilOnline());
				// The part may have arrived before the connection dropped; ask where to continue.
				try {
					session = await api.getSession(session.session_id);
				} catch (statusError) {
					if (isStatus(statusError, 404)) {
						session = await create();
						remember(session.session_id);
					}
				}
			}
		}
		// Finished parts of a cancelled upload are not reported as an accepted upload.
		signal?.throwIfAborted();
		remember(null);
		onReconnecting?.(false);
		onProgress(file.size, file.size);
		return session;
	}
}

async function resumeOrCreate(file: File, api: ResumableApi, sessionId: string | null, create: () => Promise<Session>): Promise<Session> {
	if (sessionId) {
		try {
			const session = await api.getSession(sessionId);
			if (session.size === file.size) return session;
		} catch (error) {
			if (!isStatus(error, 404) && !isStatus(error, 400)) throw error;
		}
	}
	return create();
}

/** The server API, as the upload page uses it. */
export const serverApi: ResumableApi = {
	createSession: createUploadSession,
	getSession: getUploadSession,
	sendPart: sendUploadPart,
	uploadWhole: uploadRom,
	checkDuplicate: checkUploadSessionDuplicate
};
