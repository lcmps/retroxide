import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex } from '@noble/hashes/utils.js';

/** How much of the file is read and hashed between pauses, keeping the page responsive. */
const SLICE_BYTES = 4 * 1024 * 1024;

/**
 * Returns a file's SHA-256 as lowercase hex, reading it a slice at a time so even a
 * multi-gigabyte file never sits in memory whole. Stops with an AbortError when signal aborts.
 */
export async function hashFile(file: Blob, signal?: AbortSignal): Promise<string> {
	const hasher = sha256.create();
	for (let offset = 0; offset < file.size; offset += SLICE_BYTES) {
		signal?.throwIfAborted();
		hasher.update(new Uint8Array(await file.slice(offset, offset + SLICE_BYTES).arrayBuffer()));
	}
	signal?.throwIfAborted();
	return bytesToHex(hasher.digest());
}
