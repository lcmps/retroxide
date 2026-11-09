import { hashFile } from './hash.js';

/** What the worker answers: the file's SHA-256 as lowercase hex, or why hashing failed. */
export type HashWorkerResult = { sha256: string } | { error: string };

// Receives one file (passed by reference, not copied) and answers with its SHA-256.
self.onmessage = async (event: MessageEvent<Blob>) => {
	let result: HashWorkerResult;
	try {
		result = { sha256: await hashFile(event.data) };
	} catch (error) {
		result = { error: error instanceof Error ? error.message : String(error) };
	}
	self.postMessage(result);
};
