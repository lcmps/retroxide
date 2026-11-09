import { describe, expect, it } from 'vitest';
import { loginPathFor, safeReturnPath } from './paths.js';

describe('safeReturnPath', () => {
	it('follows only share links inside the app', () => {
		expect(safeReturnPath('/share/abc_DEF-123')).toBe('/share/abc_DEF-123');
		for (const unsafe of [null, '', '/library', '//evil.example', '/share//evil.example', 'https://evil.example/share/x', '/share/\\evil', '/share/', '/share/a/b']) {
			expect(safeReturnPath(unsafe)).toBe('/library');
		}
	});
});

describe('loginPathFor', () => {
	it('brings people back to a share link after signing in', () => {
		expect(loginPathFor('/share/abc')).toBe('/login?next=%2Fshare%2Fabc');
		expect(loginPathFor('/uploads')).toBe('/login');
	});
});
