import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import type { RomDiscEntry } from '#lib/api/types.js';
import DiscSwitcher from './DiscSwitcher.svelte';

const disc = (number: number): RomDiscEntry => ({
	number,
	rom_file_id: `rf-${number}`,
	filename: `Game (Disc ${number}).chd`,
	size: 1,
	hash: { sha256: '', sha1: '', md5: '', crc32: '' },
	disc: null,
	downloads: []
});

describe('DiscSwitcher', () => {
	it('marks the selected disc and reports the one chosen', async () => {
		const onselect = vi.fn<(number: number) => void>();
		render(DiscSwitcher, { discs: [disc(1), disc(2)], selected: 2, onselect });
		expect(screen.getByRole('button', { name: 'Disc 1' }).getAttribute('aria-pressed')).toBe('false');
		expect(screen.getByRole('button', { name: 'Disc 2' }).getAttribute('aria-pressed')).toBe('true');

		await fireEvent.click(screen.getByRole('button', { name: 'Disc 1' }));
		expect(onselect).toHaveBeenCalledWith(1);
	});
});
