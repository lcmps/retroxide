import { describe, expect, it } from 'vitest';
import { librarySearchTarget } from './search.js';

const at = (path: string) => new URL(path, 'http://localhost');

describe('librarySearchTarget', () => {
	it('goes to the library from another page, adding one history entry', () => {
		expect(librarySearchTarget(at('/rom/rom-1'), '', 'ma')).toEqual({ href: '/library?q=ma', replace: false });
		expect(librarySearchTarget(at('/downloads'), 'snes', '')).toEqual({ href: '/library?platform=snes', replace: false });
	});

	it('replaces the entry while typing on the library, keeping the order', () => {
		expect(librarySearchTarget(at('/library?q=ma&sort=title&order=asc'), '', 'mar')).toEqual({
			href: '/library?sort=title&order=asc&q=mar',
			replace: true
		});
	});

	it('drops empty filters, keeping only the order', () => {
		expect(librarySearchTarget(at('/library?q=ma&platform=snes&sort=genre&order=desc'), '', '  ')).toEqual({
			href: '/library?sort=genre&order=desc',
			replace: true
		});
		expect(librarySearchTarget(at('/library?q=ma'), '', '')).toEqual({ href: '/library', replace: true });
	});

	it('does not carry another page’s query string into the library', () => {
		expect(librarySearchTarget(at('/share/abc?next=x'), '', 'zelda').href).toBe('/library?q=zelda');
	});
});

describe('librarySearchTarget on the library', () => {
	it('keeps the library’s other settings, such as an open BIOS panel', () => {
		expect(librarySearchTarget(at('/library?platform=psx&bios=open'), 'psx', 'ff')).toEqual({
			href: '/library?platform=psx&bios=open&q=ff',
			replace: true
		});
	});
});
