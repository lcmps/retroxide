import { listPlatforms } from '#lib/api/client.js';
import type { Platform } from '#lib/api/types.js';
import { logger } from '#lib/utils/logger.js';

/** A list younger than this is kept as it is by an unforced refresh. */
export const STALE_AFTER_MS = 5_000;

/**
 * The platforms present in the library, shared by the platform filter and page titles. Refreshes
 * coalesce, and one within STALE_AFTER_MS of the last successful load is skipped unless forced,
 * so navigating and switching tabs do not ask the server again and again.
 */
export class PlatformCatalog {
	items = $state<Platform[]>([]);

	readonly #load: () => Promise<Platform[]>;
	readonly #now: () => number;
	#inFlight: Promise<void> | null = null;
	#loadedAt: number | null = null;

	constructor(load: () => Promise<Platform[]> = listPlatforms, now: () => number = () => Date.now()) {
		this.#load = load;
		this.#now = now;
	}

	/** Reloads the platform list; a refresh already in flight is reused, a fresh list kept. */
	refresh({ force = false }: { force?: boolean } = {}): Promise<void> {
		if (this.#inFlight) return this.#inFlight;
		if (!force && this.#loadedAt !== null && this.#now() - this.#loadedAt < STALE_AFTER_MS) {
			return Promise.resolve();
		}
		this.#inFlight = this.#load()
			.then((items) => {
				this.items = items;
				this.#loadedAt = this.#now();
			})
			.catch((error: unknown) => logger.warn('Unable to load platforms.', error))
			.finally(() => {
				this.#inFlight = null;
			});
		return this.#inFlight;
	}

	/** The display name of a platform key, or the key itself when it is not listed. */
	nameFor(key: string): string {
		const wanted = key.toLowerCase();
		return this.items.find((platform) => platform.key.toLowerCase() === wanted)?.name ?? key;
	}
}

export const platforms = new PlatformCatalog();
