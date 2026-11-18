import { describe, expect, it, vi } from 'vitest';
import { ApiClientError } from '#lib/api/errors.js';
import type { Bootstrap, Profile } from '#lib/api/types.js';
import { BootstrapSession } from './bootstrap.svelte.js';

const profile: Profile = { user_id: 'usr_1', email: 'mario@example.com', username: null, role: 'user', locale: null, font: 'ubuntu' };
const ready: Bootstrap = { setup_required: false, appearance: { font: 'pixel' }, profile };

describe('BootstrapSession', () => {
	it('loads setup state, applies the default font, and keeps the profile', async () => {
		const applyFont = vi.fn();
		const session = new BootstrapSession(async () => ready, applyFont);
		expect(await session.load()).toBe('ready');
		expect(applyFont).toHaveBeenCalledWith('pixel');
		expect(session.profile).toEqual(profile);
	});

	it('reports setup as required and no profile when signed out', async () => {
		const session = new BootstrapSession(async () => ({ setup_required: true, appearance: { font: 'pixel' }, profile: null }), vi.fn());
		expect(await session.load()).toBe('required');
		expect(session.profile).toBeNull();
	});

	it('retries anonymously once after a stale session is refused', async () => {
		const fetch = vi
			.fn<() => Promise<Bootstrap>>()
			.mockRejectedValueOnce(new ApiClientError({ message: 'unauthorized', status: 401 }))
			.mockResolvedValueOnce({ ...ready, profile: null });
		const session = new BootstrapSession(fetch, vi.fn());
		expect(await session.load()).toBe('ready');
		expect(fetch).toHaveBeenCalledTimes(2);
		expect(session.profile).toBeNull();
	});

	it('treats an unreachable backend as configured so the usual errors apply', async () => {
		const session = new BootstrapSession(async () => { throw new Error('offline'); }, vi.fn());
		expect(await session.load()).toBe('ready');
		expect(session.profile).toBeNull();
	});
});

describe('BootstrapSession profile hand-off', () => {
	it('hands the profile over once, so a later sign-in in the same tab fetches its own', async () => {
		const session = new BootstrapSession(async () => ready, vi.fn());
		await session.load();
		expect(session.takeProfile()).toEqual(profile);
		expect(session.takeProfile()).toBeNull();
	});
});
