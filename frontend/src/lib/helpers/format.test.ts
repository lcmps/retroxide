import { describe, expect, it } from 'vitest';
import { formatBytes } from './format.js';

describe('formatBytes', () => {
	it('uses binary units with sensible precision', () => {
		expect(formatBytes(0, 'en')).toBe('0 B');
		expect(formatBytes(1023, 'en')).toBe('1,023 B');
		expect(formatBytes(1536, 'en')).toBe('1.5 KB');
		expect(formatBytes(64 * 1024 * 1024, 'en')).toBe('64 MB');
		expect(formatBytes(4.25 * 1024 ** 3, 'en')).toBe('4.3 GB');
		expect(formatBytes(-5, 'en')).toBe('0 B');
	});
});
