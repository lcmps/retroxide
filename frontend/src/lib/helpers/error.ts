import { ApiClientError, getLocalizedErrorMessage as apiGetLocalizedErrorMessage } from '#lib/api/errors.js';
import type { TranslateFunction } from '#lib/i18n/helpers.js';

/**
 * Extract a user-friendly error message from various error types.
 * Wrapper around getLocalizedErrorMessage that accepts translate function.
 *
 * Priority:
 * 1. ApiClientError with localized translation
 * 2. Generic Error message
 * 3. Fallback translation key
 *
 * @param error - The error object
 * @param fallbackKey - Translation key for fallback
 * @param fallback - Fallback text if translation key missing
 * @param translate - Translation function
 * @returns User-friendly error message
 */
export function getErrorMessage(
	error: unknown,
	fallbackKey: string,
	fallback: string,
	translate: TranslateFunction
): string {
	// Use the api/errors getLocalizedErrorMessage with correct parameter order
	return apiGetLocalizedErrorMessage(error, translate, fallbackKey, fallback);
}

/**
 * Safely extract error code from error object.
 */
export function getErrorCode(error: unknown): string | undefined {
	if (error instanceof ApiClientError) {
		return error.code;
	}
	return undefined;
}

/**
 * Safely extract HTTP status from error object.
 */
export function getErrorStatus(error: unknown): number | undefined {
	if (error instanceof ApiClientError) {
		return error.status;
	}
	return undefined;
}

// Re-export commonly used error utilities
export { getLocalizedErrorMessage, getUploadStatusRetryMessage, getUploadProcessingErrorMessage } from '#lib/api/errors.js';
