import { describe, expect, it } from 'vitest';
import type { RomDisc, RomDiscEntry } from '#lib/api/types.js';
import { fileFormat, totalSize } from './files.js';

const hash = { sha256: '', sha1: '', md5: '' };
const disc = (format: RomDisc['format']): RomDisc => ({ format, tracks: [], can_compress: false, compression_requested: false });
const entry = (filename: string, extra: Partial<RomDiscEntry> = {}): RomDiscEntry => ({ number: 1, rom_file_id: 'rf', filename, size: 10, hash, ...extra });

describe('fileFormat', () => {
	it('names a plain file by its extension', () => {
		expect(fileFormat(entry('Super Mario World (USA).sfc'))).toBe('SFC');
		expect(fileFormat(entry('game.GBA'))).toBe('GBA');
	});

	it('falls back to the first download extension, then to nothing', () => {
		expect(fileFormat(entry('noextension', { downloads: [{ format: 'file', extension: '.nes' }] }))).toBe('NES');
		expect(fileFormat(entry('noextension'))).toBe('');
	});

	it('names a disc by how it is stored', () => {
		expect(fileFormat(entry('Game.cue', { disc: disc('chd') }))).toBe('CHD');
		expect(fileFormat(entry('Game.cue', { disc: disc('tracks') }))).toBe('CUE/BIN');
		expect(fileFormat(entry('Game.gdi', { disc: disc('tracks') }))).toBe('GDI');
		expect(fileFormat(entry('Game.cue', { disc: disc('tracks'), downloads: [{ format: 'tracks', extension: '.zip', sheet: 'gdi' }] }))).toBe('GDI');
	});
});

describe('totalSize', () => {
	it('adds every file', () => {
		expect(totalSize([entry('a.iso', { size: 700 }), entry('b.iso', { size: 300 })])).toBe(1000);
		expect(totalSize([])).toBe(0);
	});
});
