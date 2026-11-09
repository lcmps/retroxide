import { describe, expect, it } from 'vitest';
import type { Bios } from '#lib/api/types.js';
import { biosLabel, biosLibraryLink, collapsedSummary, rowFile, startsOpen, uploadResultLabel } from './panel.js';

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

describe('rowFile', () => {
	it('names every row by the file emulators expect, with its note', () => {
		expect(rowFile(entry({ platform: 'dreamcast', expected_path: 'dc/dc_boot.bin', region: null, version: null, date: null, note: 'Boot ROM' }))).toEqual({
			name: 'dc_boot.bin',
			note: 'Boot ROM',
			recognized: true
		});
		expect(rowFile(entry({ expected_path: 'dump.bin', filename: 'dump.bin', recognized: false, region: null, version: null, date: '1999', note: null }))).toEqual({
			name: 'dump.bin',
			note: null,
			recognized: false
		});
	});
});

describe('opening the BIOS list from an upload result', () => {
	it('links to the platform with the list open, which otherwise starts closed', () => {
		expect(biosLibraryLink('ps2')).toBe('/library?platform=ps2&bios=open');
		expect(startsOpen(new URLSearchParams('platform=ps2&bios=open'))).toBe(true);
		expect(startsOpen(new URLSearchParams('platform=ps2'))).toBe(false);
	});
});
