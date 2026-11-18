import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import CompressionProgress from './CompressionProgress.svelte';

describe('CompressionProgress', () => {
	it('shows a waiting message with an indeterminate bar before compression starts', () => {
		render(CompressionProgress, { percent: null });
		expect(screen.getByText('Waiting to compress...')).toBeTruthy();
		expect(screen.getByRole('progressbar').hasAttribute('value')).toBe(false);
	});

	it('shows the percentage on a measurable bar while compressing', () => {
		render(CompressionProgress, { percent: 42 });
		expect(screen.getByText('Compressing to CHD: 42%')).toBeTruthy();
		expect(screen.getByRole('progressbar').getAttribute('value')).toBe('42');
	});

	it('reports the check with an indeterminate bar at 100', () => {
		render(CompressionProgress, { percent: 100 });
		expect(screen.getByText('Checking the compressed disc...')).toBeTruthy();
		expect(screen.getByRole('progressbar').hasAttribute('value')).toBe(false);
	});
});
