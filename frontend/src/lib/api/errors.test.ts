import { describe, expect, it } from 'vitest';
import { ApiClientError, getLocalizedErrorMessage } from './errors';

describe('server error diagnostics', () => {
	it('includes the request ID without exposing the internal cause', () => {
		const error = new ApiClientError({
			message: 'writing /private/storage failed',
			code: 'internal_error',
			status: 500,
			requestId: 'req_test_123'
		});
		expect(getLocalizedErrorMessage(error, (_key, fallback) => fallback, 'upload.error', 'Upload failed'))
			.toBe('Something went wrong (Request ID: req_test_123)');
	});
});