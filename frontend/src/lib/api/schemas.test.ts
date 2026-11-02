import { describe, expect, it } from 'vitest';

import { parseRomListResponse, parseSessionResponse, parseUploadAcceptedResponse } from './schemas';

describe('parseSessionResponse', () => {
	it('rejects a session payload without a refresh token', () => {
		expect(() =>
			parseSessionResponse({
				access_token: 'access-token',
				token_type: 'Bearer',
				expires_in: 900
			})
		).toThrow('Invalid session response');
	});

	it('parses a valid session payload', () => {
		expect(
			parseSessionResponse({
				access_token: 'access-token',
				refresh_token: 'refresh-token',
				token_type: 'Bearer',
				expires_in: 900
			})
		).toEqual({
			access_token: 'access-token',
			refresh_token: 'refresh-token',
			token_type: 'Bearer',
			expires_in: 900
		});
	});
});

describe('parseRomListResponse', () => {
	it('rejects ROM entries with missing metadata', () => {
		expect(() => parseRomListResponse({ item: [{ rom_id: 'rom_1' }], next_cursor: null })).toThrow(
			'Invalid ROM list response'
		);
	});
});

describe('parseUploadAcceptedResponse', () => {
	it('accepts the processing response', () => {
		expect(parseUploadAcceptedResponse({ upload_id: 'upl-1', status: 'processing' })).toEqual({
			upload_id: 'upl-1', status: 'processing'
		});
	});
});