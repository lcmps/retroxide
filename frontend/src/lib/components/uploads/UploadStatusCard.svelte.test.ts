import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import UploadStatusCard from './UploadStatusCard.svelte';

describe('UploadStatusCard', () => {
	it('links a finished upload to its ROM', () => {
		render(UploadStatusCard, { upload: { upload_id: 'upl_1', status: 'completed', rom_id: 'rom_42', error: null } });
		const link = screen.getByRole('link', { name: 'Open ROM' });
		expect(new URL((link as HTMLAnchorElement).href).pathname).toBe('/rom/rom_42');
		expect(screen.queryByText(/ROM ID:/)).toBeNull();
	});
});
