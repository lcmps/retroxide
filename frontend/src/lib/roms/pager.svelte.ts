import { listRoms, type RomListFilters } from '#lib/api/client.js';
import type { RomListResponse, RomSummary } from '#lib/api/types.js';

export type FetchRomPage = (limit: number, cursor: string | undefined, filters: RomListFilters) => Promise<RomListResponse>;

/**
 * Loads the ROM catalog one page at a time for infinite scrolling. Changing filters with reset()
 * starts over; responses to superseded filters are ignored.
 */
export class RomPager {
	items = $state<RomSummary[]>([]);
	loading = $state(false);
	/** Whether the server reported no further pages. */
	exhausted = $state(false);
	error = $state<unknown>(null);

	readonly #fetchPage: FetchRomPage;
	readonly #pageSize: number;
	#filters: RomListFilters = {};
	#cursor: string | undefined;
	#generation = 0;

	constructor(fetchPage: FetchRomPage = listRoms, pageSize = 60) {
		this.#fetchPage = fetchPage;
		this.#pageSize = pageSize;
	}

	/** Discards loaded items and loads the first page for filters. */
	reset(filters: RomListFilters = {}): Promise<void> {
		this.#generation += 1;
		this.#filters = filters;
		this.#cursor = undefined;
		this.items = [];
		this.exhausted = false;
		this.error = null;
		this.loading = false;
		return this.loadMore();
	}

	/** Appends the next page; no-op while loading, after an error, or once exhausted. */
	async loadMore(): Promise<void> {
		if (this.loading || this.exhausted || this.error) {
			return;
		}
		const generation = this.#generation;
		this.loading = true;
		try {
			const page = await this.#fetchPage(this.#pageSize, this.#cursor, this.#filters);
			if (generation !== this.#generation) {
				return;
			}
			this.items = [...this.items, ...page.item];
			this.#cursor = page.next_cursor ?? undefined;
			this.exhausted = !page.next_cursor;
		} catch (error) {
			if (generation === this.#generation) {
				this.error = error;
			}
		} finally {
			if (generation === this.#generation) {
				this.loading = false;
			}
		}
	}
}
