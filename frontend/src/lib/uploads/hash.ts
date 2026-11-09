import { createSHA256 } from 'hash-wasm';

/** How much of the file is read and hashed at a time, so memory use stays bounded. */
const SLICE_BYTES = 4 * 1024 * 1024;

/**
 * Returns a file's SHA-256 as lowercase hex, reading it a slice at a time so even a
 * multi-gigabyte file never sits in memory whole. Stops with an AbortError when signal aborts.
 *
 * Hashing runs in WebAssembly, which the page's CSP allows only in workers: call this from the
 * hash worker (see background-hash.ts), not from the page.
 */
export async function hashFile(file: Blob, signal?: AbortSignal): Promise<string> {
	signal?.throwIfAborted();
	const hasher = await createSHA256();
	for (let offset = 0; offset < file.size; offset += SLICE_BYTES) {
		signal?.throwIfAborted();
		hasher.update(new Uint8Array(await file.slice(offset, offset + SLICE_BYTES).arrayBuffer()));
	}
	signal?.throwIfAborted();
	return hasher.digest('hex');
}
