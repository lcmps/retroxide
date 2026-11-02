export type Locale = 'en' | 'pt-BR';

export type TranslationCatalog = Record<string, string>;

export type TranslationValues = Record<string, string | number>;

const SUPPORTED_LOCALES: readonly Locale[] = ['en', 'pt-BR'];

const INTERPOLATION_PATTERN = /\{([^{}]+)\}/g;

export const EMBEDDED_ENGLISH_CATALOG: TranslationCatalog = {
	'app.nav.login': 'Login',
	'app.nav.library': 'Library',
	'app.nav.dashboard': 'Dashboard',
	'home.title': 'Retroxide Frontend',
	'home.subtitle': 'Bits UI is installed and ready to use.',
	'home.health.checking': 'Checking backend health...',
	'home.health.ok': 'Backend is reachable at /health via Vite proxy.',
	'home.health.unexpected': 'Backend responded, but /health did not return expected payload.',
	'home.health.unreachable': 'Backend is unreachable. Check VITE_API_PROXY_TARGET and backend server status.',
	'home.sections.frontend.title': 'Frontend Workspace Ready',
	'home.sections.frontend.content': 'retroxide-app is configured with SvelteKit, TypeScript, and Bits UI for headless primitives.',
	'home.sections.next.title': 'Next Step',
	'home.sections.next.content': 'Start composing project screens using Bits UI components while keeping business rules in the backend API.',
	'login.title': 'Login',
	'login.subtitle': 'Creates a backend session via POST /api/v1/session.',
	'login.welcome': 'Access your Retroxide library',
	'login.cardTitle': 'Session Login',
	'login.cardDescription': 'Sign in to browse ROMs, downloads, and uploads.',
	'login.fields.email': 'Email',
	'login.fields.emailPlaceholder': 'user@example.com',
	'login.fields.password': 'Password',
	'login.fields.passwordPlaceholder': '••••••••',
	'login.actions.submit': 'Login',
	'login.actions.submitting': 'Logging in...',
	'login.decorative.email': 'Email icon',
	'login.decorative.password': 'Password icon',
	'login.messages.success': 'Logged in successfully. Token type: {tokenType}. Expires in {expiresIn}s.',
	'login.messages.failure': 'Login failed',
	'library.title': 'Library',
	'library.roms.title': 'ROM Catalog',
	'library.roms.loading': 'Loading ROMs...',
	'library.roms.empty': 'No ROMs matched the current filters.',
	'library.roms.count': '{count} ROMs',
	'library.roms.error': 'Failed to load ROMs',
	'library.roms.reset': 'Clear Filters',
	'library.roms.label.id': 'ROM ID',
	'library.roms.label.platform': 'Platform',
	'library.roms.cover': 'ROM cover placeholder',
	'downloads.queue.title': 'Download Queue',
	'downloads.queue.empty': 'No queued downloads yet.',
	'downloads.queue.count': '{count} queued ROMs',
	'downloads.catalog.title': 'Available ROMs',
	'downloads.catalog.loading': 'Loading ROMs...',
	'downloads.catalog.empty': 'No ROMs are available to queue.',
	'downloads.catalog.error': 'Failed to load ROMs',
	'downloads.catalog.count': '{count} ROMs available',
	'downloads.actions.add': 'Add to queue',
	'downloads.actions.start': 'Start download',
	'downloads.actions.remove': 'Remove',
	'downloads.messages.added': '{title} added to the queue.',
	'downloads.messages.downloaded': 'Downloaded {filename}.',
	'downloads.messages.filename': 'Filename: {filename}',
	'downloads.messages.error': 'Error: {error}',
	'downloads.status.ready': 'Ready to download',
	'uploads.form.submit': 'Upload ROM',
	'uploads.form.selected': 'Selected file: {name}',
	'uploads.form.none': 'No file selected.',
	'uploads.progress.label': 'Upload progress',
	'uploads.messages.accepted': 'Upload accepted. Tracking {uploadId}.',
	'uploads.messages.id': 'Upload ID: {uploadId}',
	'uploads.messages.status': 'Status: {status}',
	'uploads.messages.romId': 'ROM ID: {romId}',
	'uploads.messages.error': 'Error: {error}',
	'uploads.status.completed': 'Completed',
	'uploads.status.failed': 'Failed',
	'rom.title': 'ROM Detail',
	'rom.subtitle': 'Detail route for ROM id: {romId}',
	'rom.metadata.title': 'Metadata',
	'rom.actions.download': 'Download',
	'rom.actions.refresh': 'Refresh',
	'rom.actions.refreshing': 'Refreshing...',
	'rom.actions.delete': 'Delete ROM',
	'rom.actions.deleting': 'Deleting...',
	'rom.messages.loading': 'Loading...',
	'rom.messages.empty': 'No ROM data loaded.',
	'rom.messages.loadFailure': 'Failed to load ROM',
	'rom.messages.deleteSuccess': 'ROM deleted successfully.',
	'rom.messages.deleteFailure': 'Failed to delete ROM',
	'rom.fields.id': 'ROM ID',
	'rom.fields.title': 'Title',
	'rom.fields.platform': 'Platform',
	'rom.fields.region': 'Region',
	'rom.fields.version': 'Version',
	'rom.fields.sha256': 'SHA-256',
	'rom.fields.sha1': 'SHA-1',
	'rom.fields.md5': 'MD5',
	'rom.fields.crc32': 'CRC32',
	'rom.placeholders.title': 'ROM title',
	'rom.placeholders.platform': 'Platform',
	'rom.placeholders.region': 'Region',
	'rom.placeholders.version': 'Version',
	'app.shell.skip': 'Skip to content',
	'app.shell.brand': 'Retroxide',
	'app.shell.controls': 'Application controls',
	'app.shell.library_only': 'Available on the Library page only.',
	'app.shell.all_platforms': 'All platforms',
	'app.shell.search_label': 'Search library',
	'app.shell.search_placeholder': 'Search ROMs',
	'app.shell.navigation': 'Primary navigation',
	'nav.library': 'Library',
	'nav.uploads': 'Uploads',
	'nav.downloads': 'Downloads',
	'common.actions.logout': 'Log out',
	'common.status.completed': 'Completed',
	'common.status.failed': 'Failed',
	'common.status.uploading': 'Uploading...',
	'common.status.downloading': 'Downloading...',
	'common.errors.upload': 'Failed to upload ROM',
	'common.errors.download': 'Failed to download ROM',
	'common.errors.unauthorized': 'You need to sign in to continue',
	'common.errors.not_found': 'Requested resource was not found',
	'common.errors.generic': 'Something went wrong',
	'downloads.heading': 'Downloads',
	'downloads.subtitle': 'Manage ROM download history and file retrieval.',
	'uploads.heading': 'Uploads',
	'uploads.subtitle': 'Upload ROM binaries and watch identification progress.',
	'uploads.description': 'Send ROM files to the backend and monitor processing state.',
	'uploads.empty': 'No uploads in progress.',
	'uploads.pick_file': 'Choose a ROM file',
	'uploads.errors.tooLarge': 'Upload exceeds the maximum allowed size.',
	'uploads.errors.unsupportedType': 'This file type is not allowed for uploads.',
	'uploads.errors.alreadyExists': 'This ROM already exists in the library.',
	'uploads.errors.statusRetry': 'Upload status could not be refreshed. Retrying...',
	'uploads.errors.processingFailed': 'Upload processing failed.',
	'uploads.status.processing': 'Processing upload...'
};

function isLocale(value: string | null | undefined): value is Locale {
	return value === 'en' || value === 'pt-BR';
}

/**
 * Get list of supported locales.
 *
 * @returns Array of supported locale strings
 */
export function getSupportedLocales(): readonly Locale[] {
	return SUPPORTED_LOCALES;
}

/**
 * Normalize a language tag to a supported locale.
 * Handles various formats like 'en-US', 'pt', 'pt-br', etc.
 *
 * @param value - Language tag to normalize
 * @returns Normalized locale or null if not supported
 */
export function normalizeLocale(value: string | null | undefined): Locale | null {
	if (!value) {
		return null;
	}

	if (isLocale(value)) {
		return value;
	}

	const lowerCasedValue = value.toLowerCase();
	if (lowerCasedValue === 'en' || lowerCasedValue.startsWith('en-')) {
		return 'en';
	}

	if (lowerCasedValue === 'pt-br' || lowerCasedValue === 'pt' || lowerCasedValue.startsWith('pt-')) {
		return 'pt-BR';
	}

	return null;
}

/**
 * Determine the preferred locale based on user preference and browser settings.
 * Priority: stored locale > normalized browser locale > English (default)
 *
 * @param storedLocale - User-selected locale from localStorage
 * @param browserLocale - Browser's language from navigator.language
 * @returns The preferred locale
 */
export function getPreferredLocale(
	storedLocale: string | null | undefined,
	browserLocale: string | null | undefined
): Locale {
	return normalizeLocale(storedLocale) ?? normalizeLocale(browserLocale) ?? 'en';
}

/**
 * Parse and validate a translation catalog from untrusted data (e.g., API response).
 * Ensures all keys and values are strings.
 *
 * @param value - Raw data to parse
 * @returns Validated translation catalog
 * @throws Error if payload is invalid
 */
export function parseTranslationCatalog(value: unknown): TranslationCatalog {
	if (!value || typeof value !== 'object' || Array.isArray(value)) {
		throw new Error('Invalid translation catalog payload');
	}

	const catalog: TranslationCatalog = {};

	for (const [key, entry] of Object.entries(value)) {
		if (typeof entry !== 'string') {
			throw new Error(`Invalid translation value for key \"${key}\"`);
		}

		catalog[key] = entry;
	}

	return catalog;
}

/**
 * Replace interpolation placeholders in a translation string.
 * Placeholders use {key} syntax. Unknown keys are left unchanged.
 *
 * @param template - Template string with {key} placeholders
 * @param values - Object with interpolation values
 * @returns Interpolated string
 */
export function interpolateTranslation(template: string, values?: TranslationValues): string {
	if (!values) {
		return template;
	}

	return template.replace(INTERPOLATION_PATTERN, (match, key: string) => {
		const value = values[key.trim()];
		return value === undefined ? match : String(value);
	});
}

/**
 * Resolve a translation key to a value with fallback behavior.
 * Priority: active catalog > English catalog > key itself
 *
 * @param catalog - Active translation catalog
 * @param englishCatalog - English fallback catalog
 * @param key - Translation key to resolve
 * @param values - Optional interpolation values
 * @returns Translated string or key if not found
 */
export function resolveTranslation(
	catalog: TranslationCatalog,
	englishCatalog: TranslationCatalog,
	key: string,
	values?: TranslationValues
): string {
	const resolved = catalog[key] ?? englishCatalog[key] ?? key;
	return interpolateTranslation(resolved, values);
}

export function createCatalog(catalog: TranslationCatalog, englishCatalog: TranslationCatalog) {
	return {
		t(key: string, values?: TranslationValues): string {
			return resolveTranslation(catalog, englishCatalog, key, values);
		}
	};
}