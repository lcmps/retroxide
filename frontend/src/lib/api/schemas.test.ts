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

describe('parseRomListResponse disc_count', () => {
	const row = { rom_id: 'rom_1', title: 'Game', platform: 'psx', platform_name: 'PlayStation', sha256: 'abc' };

	it('keeps disc_count from the ROM list', () => {
		const parsed = parseRomListResponse({ item: [{ ...row, disc_count: 3 }], next_cursor: null });
		expect(parsed.item[0].disc_count).toBe(3);
	});

	it('treats a missing disc_count as one disc', () => {
		const parsed = parseRomListResponse({ item: [row], next_cursor: null });
		expect(parsed.item[0].disc_count).toBe(1);
	});
});

describe('parseUploadAcceptedResponse', () => {
	it('accepts the processing response', () => {
		expect(parseUploadAcceptedResponse({ upload_id: 'upl-1', status: 'processing' })).toEqual({
			upload_id: 'upl-1', status: 'processing'
		});
	});
});