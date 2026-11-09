import { ApiClientError } from '#lib/api/errors.js';
import type { SharePreview } from '#lib/api/types.js';

export type ShareLoad =
	| { phase: 'ready'; preview: SharePreview; cover: Blob | undefined }
	| { phase: 'used'; preview: null }
	| { phase: 'failed'; preview: null; error: unknown };

/**
 * Loads what a share link offers. A spent, unknown or deleted link is 'used'; the cover is a
 * nicety, so failing to load it never hides a valid offer.
 */
export async function loadShare(
	token: string,
	api: { preview: (token: string) => Promise<SharePreview>; cover: (token: string) => Promise<Blob> }
): Promise<ShareLoad> {
	let preview: SharePreview;
	try {
		preview = await api.preview(token);
	} catch (error) {
		if (error instanceof ApiClientError && error.status === 404) return { phase: 'used', preview: null };
		return { phase: 'failed', preview: null, error };
	}
	let cover: Blob | undefined;
	if (preview.has_cover) {
		cover = await api.cover(token).catch(() => undefined);
	}
	return { phase: 'ready', preview, cover };
}
