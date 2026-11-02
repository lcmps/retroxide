import { describe, expect, it } from 'vitest';
import { createLimiter } from './concurrency.js';

describe('createLimiter', () => {
	it('never runs more than the limit at once and runs every task', async () => {
		const limit = createLimiter(2);
		let active = 0;
		let peak = 0;
		const task = (value: number) =>
			limit(async () => {
				active += 1;
				peak = Math.max(peak, active);
				await new Promise((resolve) => setTimeout(resolve, 5));
				active -= 1;
				return value;
			});

		const results = await Promise.all([1, 2, 3, 4, 5].map(task));

		expect(results).toEqual([1, 2, 3, 4, 5]);
		expect(peak).toBe(2);
	});

	it('releases the slot when a task fails', async () => {
		const limit = createLimiter(1);
		await expect(limit(() => Promise.reject(new Error('boom')))).rejects.toThrow('boom');
		await expect(limit(() => Promise.resolve('next'))).resolves.toBe('next');
	});
});
