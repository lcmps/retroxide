import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import RomCover from './RomCover.svelte';

describe('RomCover', () => {
	it('renders the thumbnail URL as a lazily loaded image resolved against the API origin', () => {
		const { container } = render(RomCover, {
			romId: 'rom-1',
			coverArtUrl: '/api/v1/rom/rom-1/cover?v=0123456789abcdef&token=t',
			coverThumbUrl: '/api/v1/rom/rom-1/cover/thumb?v=fedcba9876543210&token=t'
		});
		const image = container.querySelector('img');
		expect(image).not.toBeNull();
		expect(new URL(image!.getAttribute('src')!, 'http://localhost').pathname + new URL(image!.src).search).toBe(
			'/api/v1/rom/rom-1/cover/thumb?v=fedcba9876543210&token=t'
		);
		expect(image!.getAttribute('loading')).toBe('lazy');
	});

	it('falls back to the full cover without a thumbnail and renders nothing without a cover', () => {
		const { container, unmount } = render(RomCover, { romId: 'rom-1', coverArtUrl: '/api/v1/rom/rom-1/cover?token=t' });
		expect(container.querySelector('img')!.src).toContain('/api/v1/rom/rom-1/cover?token=t');
		unmount();
		const empty = render(RomCover, { romId: 'rom-2', coverArtUrl: null });
		expect(empty.container.querySelector('img')).toBeNull();
	});

	it('hides the image when it fails to load, instead of showing a broken picture', async () => {
		const { container } = render(RomCover, { romId: 'rom-1', coverArtUrl: '/api/v1/rom/rom-1/cover?token=expired' });
		const image = container.querySelector('img')!;
		await fireEvent.error(image);
		expect(container.querySelector('img')).toBeNull();
	});
});
