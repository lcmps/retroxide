import { describe, expect, it } from 'vitest';
import { ApiClientError } from '#lib/api/errors.js';
import type { SharePreview } from '#lib/api/types.js';
import { loadShare } from './load.js';

const preview: SharePreview = { title: 'Test Quest', platform: 'nes', platform_name: 'NES', disc_count: 1, has_cover: true, already_in_library: false };
const notFound = new ApiClientError({ message: 'not found', code: 'not_found', status: 404 });

describe('loadShare', () => {
	it('shows the offer even when its cover cannot be loaded', async () => {
		const loaded = await loadShare('t', { preview: async () => preview, cover: async () => Promise.reject(notFound) });
		expect(loaded).toEqual({ phase: 'ready', preview, cover: undefined });
	});

	it('loads the cover only when there is one', async () => {
		const cover = new Blob(['png']);
		const loaded = await loadShare('t', { preview: async () => preview, cover: async () => cover });
		expect(loaded.phase === 'ready' ? loaded.cover : null).toBe(cover);
		let asked = false;
		await loadShare('t', { preview: async () => ({ ...preview, has_cover: false }), cover: async () => { asked = true; return cover; } });
		expect(asked).toBe(false);
	});

	it('tells a spent or unknown link from a failure', async () => {
		expect((await loadShare('t', { preview: async () => Promise.reject(notFound), cover: async () => new Blob() })).phase).toBe('used');
		const broken = new ApiClientError({ message: 'down', status: 500 });
		expect(await loadShare('t', { preview: async () => Promise.reject(broken), cover: async () => new Blob() })).toEqual({ phase: 'failed', preview: null, error: broken });
	});
});
