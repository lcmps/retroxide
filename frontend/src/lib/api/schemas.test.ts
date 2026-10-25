import { describe, expect, it } from 'vitest';

import { parseClientListResponse, parseLibraryDetailResponse, parseRomListResponse, parseSessionResponse, parseUploadAcceptedResponse } from './schemas';

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

describe('parseLibraryDetailResponse', () => {
	it('requires a ROM membership array', () => {
		expect(() => parseLibraryDetailResponse({ library_id: 'lib-1', name: 'SNES' })).toThrow('Invalid library response');
	});

	it('parses empty and populated memberships', () => {
		expect(parseLibraryDetailResponse({ library_id: 'lib-1', name: 'SNES', rom_ids: ['rom-1'] })).toEqual({
			library_id: 'lib-1', name: 'SNES', rom_ids: ['rom-1']
		});
	});
});

describe('parseClientListResponse', () => {
	it('keeps only public client metadata', () => {
		expect(parseClientListResponse({ item: [{ id: 'c1', user_id: 'u1', name: 'desktop', token_hash: 'private' }] })).toEqual({
			item: [{ id: 'c1', user_id: 'u1', name: 'desktop' }]
		});
	});
});

describe('parseUploadAcceptedResponse', () => {
	it('accepts the processing response', () => {
		expect(parseUploadAcceptedResponse({ upload_id: 'upl-1', status: 'processing' })).toEqual({
			upload_id: 'upl-1', status: 'processing'
		});
	});
});