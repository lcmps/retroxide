import { hashFile } from './hash.js';
import type { HashWorkerResult } from './hash.worker.js';

/**
 * Returns a file's SHA-256 as lowercase hex, computed in a Web Worker so hashing a
 * multi-gigabyte file never stalls the page. Aborting terminates the worker. Where workers are
 * unavailable, or the worker cannot start, the file is hashed on the main thread instead.
 */
export function hashFileInBackground(file: Blob, signal?: AbortSignal): Promise<string> {
	if (typeof Worker === 'undefined') return hashFile(file, signal);
	if (signal?.aborted) return Promise.reject(signal.reason);
	return new Promise((resolve, reject) => {
		const worker = new Worker(new URL('./hash.worker.ts', import.meta.url), { type: 'module' });
		const finish = () => {
			worker.terminate();
			signal?.removeEventListener('abort', abort);
		};
		const abort = () => {
			finish();
			reject(signal?.reason);
		};
		signal?.addEventListener('abort', abort, { once: true });
		worker.onmessage = (event: MessageEvent<HashWorkerResult>) => {
			finish();
			if ('sha256' in event.data) resolve(event.data.sha256);
			else reject(new Error(event.data.error));
		};
		// Hashing failures arrive as messages, so an error event means the worker itself broke
		// (it failed to load, say): fall back to hashing here.
		worker.onerror = (event) => {
			event.preventDefault();
			finish();
			hashFile(file, signal).then(resolve, reject);
		};
		worker.postMessage(file);
	});
}
