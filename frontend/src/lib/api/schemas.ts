import type { ClientListResponse, LibraryDetail, RomListResponse, SessionResponse, UploadAcceptedResponse } from './types';

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

export function parseLibraryDetailResponse(value: unknown): LibraryDetail {
	const payload = asRecord(value);
	if (!payload || typeof payload.library_id !== 'string' || typeof payload.name !== 'string' || !Array.isArray(payload.rom_ids) || !payload.rom_ids.every((id) => typeof id === 'string')) {
		throw new Error('Invalid library response');
	}
	return { library_id: payload.library_id, name: payload.name, rom_ids: payload.rom_ids };
}

export function parseClientListResponse(value: unknown): ClientListResponse {
	const payload = asRecord(value);
	if (!payload || !Array.isArray(payload.item)) throw new Error('Invalid client list response');
	const item = payload.item.map((entry) => {
		const client = asRecord(entry);
		if (!client || typeof client.id !== 'string' || typeof client.user_id !== 'string' || typeof client.name !== 'string') {
			throw new Error('Invalid client list response');
		}
		return { id: client.id, user_id: client.user_id, name: client.name };
	});
	return { item };
}