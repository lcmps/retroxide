import { browser } from '$app/env';
import { getAppearance, hasStoredSession } from '#lib/api/client.js';
import type { UIFont } from '#lib/api/types.js';

const FONT_STORAGE_KEY = 'retroxide.font';
export const FONTS: readonly UIFont[] = ['pixel', 'ubuntu', 'system'];

/** The font files to wait for before painting; the system font has none. */
const FONT_FAMILIES: Partial<Record<UIFont, string>> = {
	pixel: 'Press Start 2P',
	ubuntu: 'Ubuntu'
};

/** The application default (an admin setting) and the signed-in user's own choice. */
let defaultFont: UIFont | null = null;
let userFont: UIFont | null = null;
/** Whether the signed-in user's choice has been learned; until then the default may be wrong. */
let userFontKnown = false;

function isFont(value: unknown): value is UIFont {
	return typeof value === 'string' && (FONTS as readonly string[]).includes(value);
}

function cachedFont(): UIFont | null {
	try {
		const value = localStorage.getItem(FONT_STORAGE_KEY);
		return isFont(value) ? value : null;
	} catch {
		return null;
	}
}

function render(font: UIFont): void {
	if (font === 'pixel') {
		delete document.documentElement.dataset.font;
	} else {
		document.documentElement.dataset.font = font;
	}
}

/**
 * Shows the user's own font when they picked one, else the application default. The result is
 * cached so the next page load starts in the right font before the server answers.
 */
function applyEffectiveFont(): void {
	if (!browser) return;
	// The default answers before the profile does; showing it in between would flash the wrong
	// font, and resize the page, for every user with their own choice.
	const awaitingUser = !userFontKnown && hasStoredSession();
	const font = awaitingUser ? null : (userFont ?? defaultFont);
	if (!font) {
		render(cachedFont() ?? 'pixel');
		return;
	}
	render(font);
	try {
		localStorage.setItem(FONT_STORAGE_KEY, font);
	} catch {
		// Storage can be unavailable; the font still applies for this page.
	}
}

/** Starts in the last font this browser showed, until the server's settings arrive. */
export function applyCachedFont(): void {
	applyEffectiveFont();
}

/** Sets the application default font, which applies to users without their own choice. */
export function setDefaultFont(font: string | null | undefined): void {
	defaultFont = isFont(font) ? font : null;
	applyEffectiveFont();
}

/** Sets the signed-in user's font; null falls back to the application default. */
export function setUserFont(font: string | null | undefined): void {
	userFont = isFont(font) ? font : null;
	userFontKnown = true;
	applyEffectiveFont();
}

/** Resolves once the shown font's file has loaded, so text is first painted at its final size. */
export async function loadShownFont(): Promise<void> {
	const shown = (document.documentElement.dataset.font ?? 'pixel') as UIFont;
	const family = FONT_FAMILIES[shown];
	if (family) {
		await document.fonts.load(`1em "${family}"`);
	}
}

/** Fetches the application default font, which every visitor gets, signed in or not. */
export async function loadAppearance(): Promise<void> {
	try {
		setDefaultFont((await getAppearance()).font);
	} catch {
		// Keep the cached or default font when the server cannot be reached.
	}
}
