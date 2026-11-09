import { describe, expect, it } from 'vitest';
import type { Bios } from '#lib/api/types.js';
import { biosLabel, collapsedSummary, uploadResultLabel } from './panel.js';

const entry = (overrides: Partial<Bios>): Bios => ({
	bios_id: 'bio_1',
	platform: 'ps2',
	platform_name: 'PlayStation 2',
	filename: 'upload.bin',
	expected_path: 'pcsx2/bios/ps2-0160e-20010704.bin',
	recognized: true,
	region: 'Europe',
	version: '1.60',
	model: null,
	date: '2001-07-04',
	note: null,
	size: 4194304,
	sha256: 'a'.repeat(64),
	sha1: 'b'.repeat(40),
	md5: 'c'.repeat(32),
	crc32: '0000000d',
	can_modify: false,
	...overrides
});

describe('collapsedSummary', () => {
	it('counts the files and those that need a label', () => {
		expect(collapsedSummary([])).toEqual({ count: 0, needsLabel: 0 });
		expect(collapsedSummary([entry({}), entry({ bios_id: 'bio_2', recognized: false, region: null, version: null, date: null }), entry({ bios_id: 'bio_3', recognized: false, model: 'SCPH-39004' })])).toEqual({
			count: 3,
			needsLabel: 1
		});
	});
});

describe('biosLabel', () => {
	it('names a file by its labels, or by its filename when it has none', () => {
		expect(biosLabel(entry({}))).toBe('Europe · 1.60');
		expect(biosLabel(entry({ region: null, version: null, filename: 'dump.bin', recognized: false }))).toBe('dump.bin');
	});
});

describe('uploadResultLabel', () => {
	it('describes what an upload became', () => {
		expect(uploadResultLabel({ platform: 'ps2', platform_name: 'PlayStation 2', region: 'Europe', version: '1.60', recognized: true })).toEqual({
			platform: 'PlayStation 2',
			details: 'Europe, 1.60',
			recognized: true
		});
		expect(uploadResultLabel({ platform: 'psx', platform_name: 'PlayStation', region: null, version: null, recognized: false })).toEqual({
			platform: 'PlayStation',
			details: '',
			recognized: false
		});
	});
});
