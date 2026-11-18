import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import SignedImage from './SignedImage.svelte';

describe('SignedImage', () => {
	it('renders the resolved URL, lazily when asked', () => {
		const { container } = render(SignedImage, { url: '/api/v1/rom/r/cover?token=t', alt: 'Cover', lazy: true });
		const image = container.querySelector('img')!;
		expect(image.src).toContain('/api/v1/rom/r/cover?token=t');
		expect(image.getAttribute('alt')).toBe('Cover');
		expect(image.getAttribute('loading')).toBe('lazy');
	});

	it('hides after a load error and shows again for a new URL', async () => {
		const { container, rerender } = render(SignedImage, { url: '/a?token=old', alt: '' });
		await fireEvent.error(container.querySelector('img')!);
		expect(container.querySelector('img')).toBeNull();
		await rerender({ url: '/a?token=new', alt: '' });
		expect(container.querySelector('img')!.src).toContain('token=new');
	});

	it('renders nothing without a URL', () => {
		const { container } = render(SignedImage, { url: null, alt: '' });
		expect(container.querySelector('img')).toBeNull();
	});
});
