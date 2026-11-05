import { describe, expect, it } from 'vitest';
import serverPlatforms from '../../../../domain/rom/platform.go?raw';
import { platformBadge } from './badges.js';

/** The platforms the server knows, read from its own list so the two cannot drift apart. */
function serverPlatformKeys(): string[] {
	return [...serverPlatforms.matchAll(/\{Key: "([^"]+)"/g)].map((match) => match[1]);
}

describe('platformBadge', () => {
	it('labels the common platforms', () => {
		expect(platformBadge('psx')).toBe('PSX');
		expect(platformBadge('ps2')).toBe('PS2');
		expect(platformBadge('psp')).toBe('PSP');
		expect(platformBadge('gc')).toBe('GC');
		expect(platformBadge('genesis')).toBe('MD');
		expect(platformBadge('pokemini')).toBe('PKMN');
		expect(platformBadge('sg1000')).toBe('SG1K');
		expect(platformBadge('switch')).toBe('NSW');
		expect(platformBadge('pcecd')).toBe('PCCD');
		expect(platformBadge('a2600')).toBe('2600');
		expect(platformBadge('vectrex')).toBe('VECTREX');
	});

	it('has a label for every platform the server knows', () => {
		const keys = serverPlatformKeys();
		expect(keys.length).toBeGreaterThan(40);
		expect(keys.filter((key) => platformBadge(key) === null)).toEqual([]);
	});

	it('gives each platform its own label, short enough for the narrowest tile', () => {
		const labels = serverPlatformKeys().map((key) => platformBadge(key) as string);
		expect(new Set(labels).size).toBe(labels.length);
		expect(labels.filter((label) => label.length > 7)).toEqual([]);
	});

	it('ignores case, as the platform filter does', () => {
		expect(platformBadge('PSX')).toBe('PSX');
		expect(platformBadge('Ps2')).toBe('PS2');
	});

	it('shows no badge for a platform it does not know', () => {
		expect(platformBadge('Some Arcade Board')).toBeNull();
		expect(platformBadge('constructor')).toBeNull();
		expect(platformBadge('')).toBeNull();
	});
});
