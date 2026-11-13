import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import LibrarySort from './LibrarySort.svelte';
import type { RomSort } from '#lib/roms/sort.js';

describe('LibrarySort', () => {
	it('cycles a field from the default order to ascending, descending, and back', async () => {
		const onchange = vi.fn<(sort: RomSort) => void>();
		const { rerender } = render(LibrarySort, { sort: null, onchange });

		await fireEvent.click(screen.getByRole('button', { name: 'Sort by Title' }));
		expect(onchange).toHaveBeenLastCalledWith({ field: 'title', order: 'asc' });

		await rerender({ sort: { field: 'title', order: 'asc' }, onchange });
		await fireEvent.click(screen.getByRole('button', { name: /Sorted by Title, ascending/ }));
		expect(onchange).toHaveBeenLastCalledWith({ field: 'title', order: 'desc' });

		await rerender({ sort: { field: 'title', order: 'desc' }, onchange });
		await fireEvent.click(screen.getByRole('button', { name: /Sorted by Title, descending/ }));
		expect(onchange).toHaveBeenLastCalledWith(null);
	});

	it('starts another field ascending whatever the current order', async () => {
		const onchange = vi.fn<(sort: RomSort) => void>();
		render(LibrarySort, { sort: { field: 'title', order: 'desc' }, onchange });
		await fireEvent.click(screen.getByRole('button', { name: 'Sort by Genre' }));
		expect(onchange).toHaveBeenLastCalledWith({ field: 'genre', order: 'asc' });
	});
});
