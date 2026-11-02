import { browser } from '$app/environment';

import { fetchCatalog } from '$lib/api/client';
import { logger } from '$lib/utils/logger';

import {
	createCatalog,
	EMBEDDED_ENGLISH_CATALOG,
	getPreferredLocale,
	getSupportedLocales,
	type Locale,
	type TranslationCatalog,
	type TranslationValues
} from './catalog';

const LOCALE_STORAGE_KEY = 'retroxide.locale';

type I18nState = {
	locale: Locale;
	desiredLocale: Locale;
	englishCatalog: TranslationCatalog;
	activeCatalog: TranslationCatalog;
	englishCatalogReady: boolean;
	hasExplicitSelection: boolean;
	initialized: boolean;
	initializing: boolean;
};

function readStoredLocale(): string | null {
	if (!browser) {
		return null;
	}

	return localStorage.getItem(LOCALE_STORAGE_KEY);
}

function readBrowserLocale(): string | null {
	if (!browser) {
		return null;
	}

	return navigator.language;
}

function persistLocale(locale: Locale): void {
	if (!browser) {
		return;
	}

	localStorage.setItem(LOCALE_STORAGE_KEY, locale);
}

const initialLocale = getPreferredLocale(undefined, undefined);

const state = $state<I18nState>({
	locale: initialLocale,
	desiredLocale: initialLocale,
	englishCatalog: EMBEDDED_ENGLISH_CATALOG,
	activeCatalog: EMBEDDED_ENGLISH_CATALOG,
	englishCatalogReady: false,
	hasExplicitSelection: false,
	initialized: false,
	initializing: false
});

let englishCatalogPromise: Promise<TranslationCatalog> | null = null;
let latestLocaleRequestId = 0;
let pendingInitialization: Promise<void> | null = null;

function getActiveState(): I18nState | null {
	return browser ? state : null;
}

async function ensureEnglishCatalog(activeState: I18nState): Promise<TranslationCatalog> {
	if (activeState.englishCatalogReady) {
		return activeState.englishCatalog;
	}

	if (!englishCatalogPromise) {
		englishCatalogPromise = (async () => {
			try {
				return await fetchCatalog('en');
			} catch (error) {
				logger.warn('Unable to load the English translation catalog; using embedded strings.', error);
				return EMBEDDED_ENGLISH_CATALOG;
			}
		})();
	}

	const englishCatalog = await englishCatalogPromise;
	activeState.englishCatalog = englishCatalog;
	activeState.englishCatalogReady = true;
	englishCatalogPromise = null;

	return englishCatalog;
}

/**
 * Synchronize the active catalog with the desired locale.
 * Uses request IDs to prevent race conditions from concurrent setLocale() calls.
 * If the target locale fetch fails, falls back to the English catalog.
 *
 * @param activeState - The current i18n state
 */
async function syncActiveCatalog(activeState: I18nState): Promise<void> {
	const requestId = ++latestLocaleRequestId;
	const targetLocale = activeState.desiredLocale;
	const englishCatalog = await ensureEnglishCatalog(activeState);

	// Check if this request is outdated (newer request came in)
	if (requestId !== latestLocaleRequestId || targetLocale !== activeState.desiredLocale) {
		return;
	}

	if (targetLocale === 'en') {
		activeState.locale = 'en';
		activeState.activeCatalog = englishCatalog;
		return;
	}

	let nextCatalog = englishCatalog;

	try {
		nextCatalog = await fetchCatalog(targetLocale);
	} catch (error) {
		logger.warn(`Unable to load the ${targetLocale} translation catalog; using English strings.`, error);
		// Gracefully fall back to English if fetch fails
		nextCatalog = englishCatalog;
	}

	// Check again in case request became outdated during fetch
	if (requestId !== latestLocaleRequestId || targetLocale !== activeState.desiredLocale) {
		return;
	}

	activeState.locale = targetLocale;
	activeState.activeCatalog = nextCatalog;
}

export const i18n = {
	get locale(): Locale {
		return getActiveState()?.locale ?? initialLocale;
	},

	get locales(): readonly Locale[] {
		return getSupportedLocales();
	},

	get initialized(): boolean {
		return getActiveState()?.initialized ?? false;
	},

	t(key: string, values?: TranslationValues): string {
		const activeState = getActiveState();
		return createCatalog(
			activeState?.activeCatalog ?? EMBEDDED_ENGLISH_CATALOG,
			activeState?.englishCatalog ?? EMBEDDED_ENGLISH_CATALOG
		).t(key, values);
	},

	/**
	 * Check if a translation key exists in the active catalog.
	 * Falls back to English catalog if key missing in active catalog.
	 *
	 * @param key - The translation key to check
	 * @returns True if the key is available
	 */
	hasTranslation(key: string): boolean {
		const activeState = getActiveState();
		const activeCatalog = activeState?.activeCatalog ?? EMBEDDED_ENGLISH_CATALOG;
		const englishCatalog = activeState?.englishCatalog ?? EMBEDDED_ENGLISH_CATALOG;

		// Check active catalog first, then English fallback
		return key in activeCatalog || key in englishCatalog;
	},

	async initialize(): Promise<void> {
		const activeState = getActiveState();

		if (!activeState) {
			return;
		}

		if (activeState.initialized) {
			return;
		}

		if (pendingInitialization) {
			return pendingInitialization;
		}

		activeState.initializing = true;

		pendingInitialization = (async () => {
			if (!activeState.hasExplicitSelection) {
				const preferredLocale = getPreferredLocale(readStoredLocale(), readBrowserLocale());
				activeState.desiredLocale = preferredLocale;
				activeState.locale = preferredLocale;
			}

			await syncActiveCatalog(activeState);

			persistLocale(activeState.desiredLocale);
			activeState.initialized = true;
		})().finally(() => {
			activeState.initializing = false;
			pendingInitialization = null;
		});

		return pendingInitialization;
	},

	async setLocale(locale: Locale): Promise<void> {
		const activeState = getActiveState();

		if (!activeState) {
			return;
		}

		if (locale === activeState.desiredLocale && activeState.initialized) {
			persistLocale(locale);
			return;
		}

		activeState.hasExplicitSelection = true;
		activeState.desiredLocale = locale;
		activeState.locale = locale;

		persistLocale(locale);
		await syncActiveCatalog(activeState);
		activeState.initialized = activeState.englishCatalogReady;
	}
};