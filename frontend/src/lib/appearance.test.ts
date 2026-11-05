import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('$app/env', () => ({ browser: true }));

const hasStoredSession = vi.fn(() => false);
vi.mock('#lib/api/client.js', () => ({
	getAppearance: vi.fn(),
	hasStoredSession: () => hasStoredSession()
}));

const stored = new Map<string, string>();
const dataset: Record<string, string> = {};

async function freshModule() {
	vi.resetModules();
	return import('./appearance');
}

const shownFont = () => dataset.font ?? 'pixel';

beforeEach(() => {
	stored.clear();
	for (const key of Object.keys(dataset)) delete dataset[key];
	hasStoredSession.mockReturnValue(false);
	vi.stubGlobal('document', { documentElement: { dataset } });
	vi.stubGlobal('localStorage', {
		getItem: (key: string) => stored.get(key) ?? null,
		setItem: (key: string, value: string) => stored.set(key, value)
	});
});

describe('the interface font while the page loads', () => {
	it('keeps the cached font until a signed-in user’s own choice is known', async () => {
		stored.set('retroxide.font', 'ubuntu');
		hasStoredSession.mockReturnValue(true);
		const { applyCachedFont, setDefaultFont, setUserFont } = await freshModule();

		applyCachedFont();
		expect(shownFont()).toBe('ubuntu');

		setDefaultFont('pixel');
		expect(shownFont()).toBe('ubuntu');
		expect(stored.get('retroxide.font')).toBe('ubuntu');

		setUserFont('ubuntu');
		expect(shownFont()).toBe('ubuntu');
	});

	it('uses the application default once a signed-in user turns out to have no choice', async () => {
		stored.set('retroxide.font', 'ubuntu');
		hasStoredSession.mockReturnValue(true);
		const { setDefaultFont, setUserFont } = await freshModule();

		setDefaultFont('pixel');
		setUserFont(null);

		expect(shownFont()).toBe('pixel');
		expect(stored.get('retroxide.font')).toBe('pixel');
	});

	it('applies the application default straight away without a session', async () => {
		stored.set('retroxide.font', 'ubuntu');
		const { setDefaultFont } = await freshModule();

		setDefaultFont('system');

		expect(shownFont()).toBe('system');
	});
});
