import type { RomListResponse, SessionResponse, UploadAcceptedResponse } from './types';

function asRecord(value: unknown): Record<string, unknown> | null {
	return value && typeof value === 'object' && !Array.isArray(value)
		? (value as Record<string, unknown>)
		: null;
}

export function parseSessionResponse(value: unknown): SessionResponse {
	const payload = asRecord(value);

	if (
		!payload ||
		typeof payload.access_token !== 'string' ||
		typeof payload.refresh_token !== 'string' ||
		typeof payload.token_type !== 'string' ||
		typeof payload.expires_in !== 'number' ||
		!Number.isSafeInteger(payload.expires_in) ||
		payload.expires_in <= 0
	) {
		throw new Error('Invalid session response');
	}

	return {
		access_token: payload.access_token,
		refresh_token: payload.refresh_token,
		token_type: payload.token_type,
		expires_in: payload.expires_in
	};
}

export function parseRomListResponse(value: unknown): RomListResponse {
	const payload = asRecord(value);
	if (!payload || !Array.isArray(payload.item) || !(typeof payload.next_cursor === 'string' || payload.next_cursor === null)) {
		throw new Error('Invalid ROM list response');
	}

	const item = payload.item.map((entry) => {
		const rom = asRecord(entry);
		if (!rom || typeof rom.rom_id !== 'string' || typeof rom.title !== 'string' || typeof rom.platform !== 'string' || typeof rom.sha256 !== 'string') {
			throw new Error('Invalid ROM list response');
		}
		if (!(typeof rom.cover_art_url === 'string' || rom.cover_art_url === null || rom.cover_art_url === undefined)) throw new Error('Invalid ROM list response');
		return { rom_id: rom.rom_id, title: rom.title, platform: rom.platform, sha256: rom.sha256, cover_art_url: rom.cover_art_url ?? null };
	});

	return { item, next_cursor: payload.next_cursor };
}

export function parseUploadAcceptedResponse(value: unknown): UploadAcceptedResponse {
	const payload = asRecord(value);
	if (!payload || typeof payload.upload_id !== 'string' || payload.status !== 'processing') {
		throw new Error('Invalid upload response');
	}
	return { upload_id: payload.upload_id, status: 'processing' };
}
