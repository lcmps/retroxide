import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('$app/env', () => ({ browser: true }));

import { createDownloadLink, fetchRomCover, getRom, hasStoredSession, logoutSession, persistStoredSession, uploadRom } from './client';

function jsonResponse(payload: unknown, status = 200): Response {
	return new Response(JSON.stringify(payload), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

describe('authenticated API requests', () => {
	beforeEach(() => {
		const values = new Map<string, string>();
		vi.stubGlobal('sessionStorage', {
			getItem: (key: string) => values.get(key) ?? null,
			setItem: (key: string, value: string) => values.set(key, value),
			removeItem: (key: string) => values.delete(key)
		});
		vi.stubGlobal('window', new EventTarget());
		vi.restoreAllMocks();
	});

	it('shares one refresh between concurrent unauthorized requests', async () => {
		persistStoredSession({
			access_token: 'old-access',
			refresh_token: 'old-refresh',
			token_type: 'Bearer',
			expires_in: 900
		});

		let refreshCalls = 0;
		const fetchMock = vi.fn<typeof fetch>(async (input, init) => {
			const url = String(input);
			const authorization = new Headers(init?.headers).get('Authorization');
			if (url.endsWith('/session/refresh')) {
				refreshCalls += 1;
				await new Promise((resolve) => setTimeout(resolve, 10));
				return jsonResponse({
					access_token: 'new-access',
					refresh_token: 'new-refresh',
					token_type: 'Bearer',
					expires_in: 900
				});
			}
			if (authorization === 'Bearer old-access') return jsonResponse({}, 401);
			if (authorization === 'Bearer new-access') {
				return jsonResponse({ rom_id: url.split('/').at(-1), title: 'Game' });
			}
			return jsonResponse({}, 401);
		});
		vi.stubGlobal('fetch', fetchMock);

		await Promise.all([getRom('rom-1'), getRom('rom-2')]);

		expect(refreshCalls).toBe(1);
		expect(fetchMock).toHaveBeenCalledTimes(5);
	});

	it('refreshes once and retries download link requests', async () => {
		persistStoredSession({ access_token: 'old-access', refresh_token: 'old-refresh', token_type: 'Bearer', expires_in: 900 });
		let refreshCalls = 0;
		const fetchMock = vi.fn<typeof fetch>(async (input, init) => {
			const authorization = new Headers(init?.headers).get('Authorization');
			if (String(input).endsWith('/session/refresh')) {
				refreshCalls++;
				return jsonResponse({ access_token: 'new-access', refresh_token: 'new-refresh', token_type: 'Bearer', expires_in: 900 });
			}
			if (authorization === 'Bearer old-access') return jsonResponse({}, 401);
			expect(String(input)).toBe('/api/v1/rom/rom-1/download-link');
			expect(init?.method).toBe('POST');
			return jsonResponse({ url: '/api/v1/download?token=signed', expires_in: 60 });
		});
		vi.stubGlobal('fetch', fetchMock);

		const link = await createDownloadLink('rom-1');

		expect(refreshCalls).toBe(1);
		expect(new URL(link.url).pathname + new URL(link.url).search).toBe('/api/v1/download?token=signed');
		expect(link.filename).toBe('rom-1.rom');
		expect(link.expiresIn).toBe(60);
	});

	it('keeps the cover content version so the HTTP cache can serve it', async () => {
		persistStoredSession({ access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', expires_in: 900 });
		const fetchMock = vi.fn<typeof fetch>(async () => new Response('png'));
		vi.stubGlobal('fetch', fetchMock);

		await fetchRomCover('rom-1', '/api/v1/rom/rom-1/cover?v=0123456789abcdef');

		expect(fetchMock).toHaveBeenCalledWith('/api/v1/rom/rom-1/cover?v=0123456789abcdef', expect.anything());
	});

	it('revokes the server session and always clears local credentials', async () => {
		persistStoredSession({ access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', expires_in: 900 });
		const fetchMock = vi.fn<typeof fetch>(async () => new Response(null, { status: 204 }));
		vi.stubGlobal('fetch', fetchMock);

		await logoutSession();

		expect(fetchMock).toHaveBeenCalledWith('/api/v1/session', expect.objectContaining({ method: 'DELETE' }));
		expect(hasStoredSession()).toBe(false);
	});

	it('clears local credentials when server-side logout fails', async () => {
		persistStoredSession({ access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', expires_in: 900 });
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => jsonResponse({ code: 'internal_error', message: 'failure' }, 500)));

		await expect(logoutSession()).rejects.toThrow();
		expect(hasStoredSession()).toBe(false);
	});

	it('refreshes and retries XHR uploads without losing progress reporting', async () => {
		persistStoredSession({ access_token: 'old-access', refresh_token: 'old-refresh', token_type: 'Bearer', expires_in: 900 });
		const fetchMock = vi.fn<typeof fetch>(async () => jsonResponse({ access_token: 'new-access', refresh_token: 'new-refresh', token_type: 'Bearer', expires_in: 900 }));
		vi.stubGlobal('fetch', fetchMock);
		let sendCount = 0;
		class FakeXMLHttpRequest extends EventTarget {
			upload = new EventTarget();
			status = 0;
			responseText = '';
			responseType = '';
			private headers = new Map<string, string>();
			open(): void {}
			setRequestHeader(name: string, value: string): void { this.headers.set(name, value); }
			send(): void {
				sendCount++;
				this.upload.dispatchEvent(Object.assign(new Event('progress'), { lengthComputable: true, loaded: 50, total: 100 }));
				if (sendCount === 1) {
					this.status = 401;
					this.responseText = JSON.stringify({ code: 'unauthorized', message: 'unauthorized' });
				} else {
					expect(this.headers.get('Authorization')).toBe('Bearer new-access');
					this.status = 202;
					this.responseText = JSON.stringify({ upload_id: 'upl-1', status: 'processing' });
				}
				this.dispatchEvent(new Event('load'));
			}
		}
		vi.stubGlobal('XMLHttpRequest', FakeXMLHttpRequest);
		const progressHandler = vi.fn();
		const result = await uploadRom(new File(['rom'], 'game.sfc'), progressHandler);

		expect(sendCount).toBe(2);
		expect(fetchMock).toHaveBeenCalledTimes(1);
		expect(result.upload_id).toBe('upl-1');
		expect(progressHandler).toHaveBeenCalled();
	});
});
