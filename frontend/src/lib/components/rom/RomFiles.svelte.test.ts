import { render, screen, within } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import type { RomDiscEntry } from '#lib/api/types.js';
import RomFiles from './RomFiles.svelte';

const file = (number: number, filename: string, size: number, extra: Partial<RomDiscEntry> = {}): RomDiscEntry => ({
	number,
	rom_file_id: `rf-${number}`,
	filename,
	size,
	hash: { sha256: 's'.repeat(64), sha1: `sha1-${number}`, md5: 'm', crc32: `crc-${number}` },
	...extra
});

describe('RomFiles', () => {
	it('shows a single file with its format, size and hashes, without a disc column', () => {
		render(RomFiles, { files: [file(1, 'Super Mario World.sfc', 524288)] });
		expect(screen.getByRole('heading', { name: 'File' })).toBeTruthy();
		const table = screen.getByRole('table');
		expect(within(table).queryByRole('columnheader', { name: 'Disc' })).toBeNull();
		const row = within(table).getAllByRole('row')[1];
		for (const text of ['Super Mario World.sfc', 'SFC', '512 KB', 'sha1-1', 'crc-1']) {
			expect(within(row).getByText(text)).toBeTruthy();
		}
	});

	it('lists every disc of a title with its own format and size, and the total', () => {
		render(RomFiles, {
			files: [
				file(1, 'Game (Disc 1).chd', 400 * 1024 * 1024, { disc: { format: 'chd', tracks: [], can_compress: false, compression_requested: false } }),
				file(2, 'Game (Disc 2).cue', 600 * 1024 * 1024, { disc: { format: 'tracks', tracks: [], can_compress: false, compression_requested: false } })
			],
			selected: 2
		});
		expect(screen.getByRole('heading', { name: 'Files' })).toBeTruthy();
		const rows = screen.getAllByRole('row');
		expect(within(rows[1]).getByText('CHD')).toBeTruthy();
		expect(within(rows[1]).getByText('400 MB')).toBeTruthy();
		expect(within(rows[2]).getByText('CUE/BIN')).toBeTruthy();
		expect(within(rows[2]).getByText('600 MB')).toBeTruthy();
		expect(rows[2].getAttribute('aria-current')).toBe('true');
		expect(rows[1].hasAttribute('aria-current')).toBe(false);
		expect(within(rows.at(-1)!).getByText('1,000 MB')).toBeTruthy();
	});
});
