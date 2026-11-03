import { listPlatforms } from '#lib/api/client.js';
import type { Platform } from '#lib/api/types.js';
import { logger } from '#lib/utils/logger.js';

/**
 * The platforms present in the library, shared by the platform filter and page titles. Refreshes
 * are cheap and coalesce, so callers refresh whenever the library may have changed.
 */
export class PlatformCatalog {
	items = $state<Platform[]>([]);

	readonly #load: () => Promise<Platform[]>;
	#inFlight: Promise<void> | null = null;

	constructor(load: () => Promise<Platform[]> = listPlatforms) {
		this.#load = load;
	}

	/** Reloads the platform list; a refresh already in flight is reused. */
	refresh(): Promise<void> {
		this.#inFlight ??= this.#load()
			.then((items) => {
				this.items = items;
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
