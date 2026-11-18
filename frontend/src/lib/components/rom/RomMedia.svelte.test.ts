import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import RomMedia from './RomMedia.svelte';

describe('RomMedia', () => {
	it('shows the title screen and the screenshot with captions', () => {
		render(RomMedia, { title: 'Zelda', titleScreenUrl: '/t?token=x', screenshotUrl: '/s?token=x' });
		expect(screen.getByRole('heading', { name: 'Screenshots' })).toBeTruthy();
		expect(screen.getByAltText('Title screen of Zelda')).toBeTruthy();
		expect(screen.getByAltText('Screenshot of Zelda')).toBeTruthy();
		expect(screen.getByText('Title screen')).toBeTruthy();
		expect(screen.getByText('In game')).toBeTruthy();
	});

	it('shows only the image there is', () => {
		render(RomMedia, { title: 'Zelda', screenshotUrl: '/s?token=x' });
		expect(screen.queryByAltText('Title screen of Zelda')).toBeNull();
		expect(screen.getByAltText('Screenshot of Zelda')).toBeTruthy();
	});

	it('renders nothing, not even a heading, without images', () => {
		const { container } = render(RomMedia, { title: 'Zelda', titleScreenUrl: null, screenshotUrl: null });
		expect(container.textContent?.trim()).toBe('');
	});
});
