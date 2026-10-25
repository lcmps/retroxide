import { interpolateTranslation, type Locale, type TranslationValues } from './catalog';

/**
 * Translation function that checks if a key has a translation.
 * Returns fallback if the key is not found in the catalog.
 */
export type TranslateFunction = (
	key: string,
	fallback: string,
	values?: TranslationValues
) => string;

/**
 * Factory function to create a type-safe translation helper.
 * Handles fallback behavior when translations are missing.
 *
 * @param i18n - The i18n store instance
 * @returns A translation function with fallback support
 */
export function useTranslate(i18n: {
	t(key: string, values?: TranslationValues): string;
	hasTranslation(key: string): boolean;
}): TranslateFunction {
	return (key: string, fallback: string, values?: TranslationValues): string => {
		if (i18n.hasTranslation(key)) {
			return i18n.t(key, values);
		}
		return interpolateTranslation(fallback, values);
	};
}

/**
 * Hook for managing common translation initialization.
 * Ensures i18n is ready before rendering translated content.
 */
export function useI18nReady(i18n: {
	initialized: boolean;
	initialize(): Promise<void>;
}): {
	isReady: boolean;
	initialize: () => Promise<void>;
} {
	return {
		isReady: i18n.initialized,
		initialize: () => i18n.initialize()
	};
}
