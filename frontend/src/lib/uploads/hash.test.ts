import { describe, expect, it } from 'vitest';
import { hashFile } from './hash.js';

/** The reference SHA-256, from the platform's own implementation. */
async function webCryptoSHA256(content: Uint8Array<ArrayBuffer>): Promise<string> {
	const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', content));
	return Array.from(digest, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

describe('hashFile', () => {
	it('matches SHA-256 across slice boundaries', async () => {
		const content = new Uint8Array(9 * 1024 * 1024 + 123).map((_, index) => (index * 31) & 0xff);
		await expect(hashFile(new Blob([content]))).resolves.toBe(await webCryptoSHA256(content));
		await expect(hashFile(new Blob([]))).resolves.toBe(await webCryptoSHA256(new Uint8Array(0)));
	});

	it('stops when aborted', async () => {
		const controller = new AbortController();
		controller.abort();
		await expect(hashFile(new Blob([new Uint8Array(10)]), controller.signal)).rejects.toThrow();
	});
});
