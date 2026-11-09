import type { ApiError } from './types';

type Translate = (key: string, fallback: string, values?: Record<string, string | number>) => string;

type ApiClientErrorInit = {
	message: string;
	code?: string;
	status?: number;
	requestId?: string;
};

const ERROR_TRANSLATIONS: Record<string, { key: string; fallback: string }> = {
	unauthorized: {
		key: 'common.errors.unauthorized',
		fallback: 'You need to sign in to continue'
	},
	invalid_credentials: {
		key: 'login.messages.failure',
		fallback: 'Login failed'
	},
	invalid_refresh_token: {
		key: 'common.errors.unauthorized',
		fallback: 'You need to sign in to continue'
	},
	not_found: {
		key: 'common.errors.not_found',
		fallback: 'Requested resource was not found'
	},
	upload_too_large: {
		key: 'uploads.errors.tooLarge',
		fallback: 'Upload exceeds the maximum allowed size.'
	},
	insufficient_storage: {
		key: 'uploads.errors.diskFull',
		fallback: 'The server does not have enough free disk space for this ROM.'
	},
	upload_type_not_allowed: {
		key: 'uploads.errors.unsupportedType',
		fallback: 'This file type is not allowed for uploads.'
	},
	rom_already_exists: {
		key: 'uploads.errors.alreadyExists',
		fallback: 'This ROM already exists in the library.'
	},
	bios_already_exists: {
		key: 'uploads.errors.biosAlreadyExists',
		fallback: 'This BIOS is already on the server.'
	},
	rate_limited: {
		key: 'common.errors.rate_limited',
		fallback: 'Too many attempts. Please wait a moment and try again.'
	},
	internal_error: {
		key: 'common.errors.generic',
		fallback: 'Something went wrong'
	},
	forbidden: {
		key: 'common.errors.generic',
		fallback: 'Something went wrong'
	},
	conflict: {
		key: 'common.errors.generic',
		fallback: 'Something went wrong'
	}
};

export class ApiClientError extends Error {
	readonly code?: string;
	readonly status?: number;
	readonly requestId?: string;

	constructor({ message, code, status, requestId }: ApiClientErrorInit) {
		super(message);
		this.name = 'ApiClientError';
		this.code = code;
		this.status = status;
		this.requestId = requestId;
	}
}

export function parseApiErrorPayload(payload: unknown): ApiError | null {
	if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
		return null;
	}

	const { code, message, request_id: requestId } = payload as Record<string, unknown>;
	if (typeof code !== 'string' || typeof message !== 'string') {
		return null;
	}

	return {
		code,
		message,
		request_id: typeof requestId === 'string' ? requestId : undefined
	};
}

export function isLocaleNeutralMessage(message: string): boolean {
	const normalizedMessage = message.trim();
	if (!normalizedMessage) {
		return false;
	}

	return (
		/^(?:[A-F0-9]{8,}|[a-z0-9_-]{8,}|[0-9]+(?:\.[0-9]+)?(?:\s?(?:B|KB|MB|GB|TB|%))?)$/i.test(normalizedMessage) ||
		/^(?:request[_ -]?id|upload[_ -]?id|rom[_ -]?id|sha(?:-)?(?:1|256)|md5|crc32)[:=]\s*[-A-Za-z0-9_]+$/i.test(normalizedMessage)
	);
}

export function getLocalizedErrorMessage(
	error: unknown,
	translate: Translate,
	fallbackKey: string,
	fallback: string
): string {
	if (error instanceof ApiClientError) {
		if (error.status !== undefined && error.status >= 500 && error.requestId) {
			const message = translate('common.errors.generic', 'Something went wrong');
			return `${message} (${translate('common.errors.requestId', 'Request ID')}: ${error.requestId})`;
		}
		const translation = error.code ? ERROR_TRANSLATIONS[error.code] : undefined;
		if (translation) {
			return translate(translation.key, translation.fallback);
		}

		if (isLocaleNeutralMessage(error.message)) {
			return error.message;
		}

		return translate(fallbackKey, fallback);
	}

	if (error instanceof Error && isLocaleNeutralMessage(error.message)) {
		return error.message;
	}

	return translate(fallbackKey, fallback);
}

export function getUploadStatusRetryMessage(translate: Translate): string {
	return translate('uploads.errors.statusRetry', 'Upload status could not be refreshed. Retrying...');
}

export function getUploadProcessingErrorMessage(rawError: string, translate: Translate): string {
	if (isLocaleNeutralMessage(rawError)) {
		return rawError;
	}

	return translate('uploads.errors.processingFailed', 'Upload processing failed.');
}