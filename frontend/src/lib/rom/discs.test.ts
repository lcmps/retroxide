import { describe, expect, it } from 'vitest';
import type { RomDisc, RomDiscEntry } from '#lib/api/types.js';
import { compressionOutcome, pickDisc } from './discs.js';

function disc(format: RomDisc['format'], requested = false): RomDisc {
	return { format, tracks: [], can_compress: false, compression_requested: requested };
}

function entry(number: number, d: RomDisc | null = null): RomDiscEntry {
	return { number, rom_file_id: `file-${number}`, filename: `Disc ${number}.cue`, size: 1, hash: { sha256: '', sha1: '', md5: '' }, disc: d };
}

describe('pickDisc', () => {
	const discs = [entry(1), entry(2), entry(3)];

	it('keeps the chosen disc while it still exists', () => {
		expect(pickDisc(discs, 2)?.number).toBe(2);
	});

	it('falls back to the first disc when the chosen one is gone', () => {
		expect(pickDisc(discs, 9)?.number).toBe(1);
		expect(pickDisc(discs, null)?.number).toBe(1);
	});

	it('returns null for an empty list', () => {
		expect(pickDisc([], 1)).toBeNull();
	});
});

describe('compressionOutcome', () => {
	it('reports running while any requested disc is still waiting', () => {
		const discs = [entry(1, disc('chd')), entry(2, disc('tracks', true))];
		expect(compressionOutcome(['file-1', 'file-2'], discs)).toBe('running');
	});

	it('reports compressed only when every requested disc became a CHD', () => {
		expect(compressionOutcome(['file-1', 'file-2'], [entry(1, disc('chd')), entry(2, disc('chd'))])).toBe('compressed');
	});

	it('reports failed when a requested disc stopped waiting without becoming a CHD', () => {
		expect(compressionOutcome(['file-1', 'file-2'], [entry(1, disc('chd')), entry(2, disc('tracks'))])).toBe('failed');
	});
});
