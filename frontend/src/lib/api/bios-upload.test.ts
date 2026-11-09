import { describe, expect, it } from 'vitest';
import { sessionRequestBody, uploadPath } from './client';

describe('BIOS upload requests', () => {
	it('marks a single-request upload as BIOS files in the query string', () => {
		expect(uploadPath()).toBe('/upload');
		expect(uploadPath({ platform: 'ps2' })).toBe('/upload?bios=true&platform=ps2');
	});

	it('marks an upload sent in parts as BIOS files in its session', () => {
		const file = new File(['bios'], 'scph5501.bin', { type: 'application/octet-stream' });
		expect(sessionRequestBody(file, false)).toEqual({ filename: 'scph5501.bin', size: 4, mime_type: 'application/octet-stream' });
		expect(sessionRequestBody(file, false, { platform: 'psx' })).toEqual({
			filename: 'scph5501.bin',
			size: 4,
			mime_type: 'application/octet-stream',
			bios: true,
			platform: 'psx'
		});
		expect(sessionRequestBody(file, true)).toMatchObject({ part: true });
	});
});
