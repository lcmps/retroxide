import type { HashWorkerResult } from './hash.worker.js';

/**
 * Returns a file's SHA-256 as lowercase hex, computed in a Web Worker so hashing a
 * multi-gigabyte file never stalls the page. Aborting terminates the worker.
 *
 * Without a working worker this rejects rather than hashing on the page: the page's CSP blocks
 * WebAssembly there, and the hash only lets the upload stop early on a duplicate, which the
 * server catches anyway.
 */
export function hashFileInBackground(file: Blob, signal?: AbortSignal): Promise<string> {
	if (typeof Worker === 'undefined') return Promise.reject(new Error('Web workers are unavailable.'));
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
		// Hashing failures arrive as messages, so an error event means the worker itself broke.
		worker.onerror = (event) => {
			event.preventDefault();
			finish();
			reject(new Error(`The hash worker failed: ${event.message}`));
		};
		worker.postMessage(file);
	});
}
