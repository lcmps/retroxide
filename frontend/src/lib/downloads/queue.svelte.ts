import type { RomSummary } from '#lib/api/types.js';

export type DownloadStatus = 'ready' | 'downloading' | 'completed' | 'failed';

export type DownloadQueueItem = {
	romId: string;
	title: string;
	platform: string;
	status: DownloadStatus;
	filename: string | null;
	error: string | null;
};

export const DOWNLOAD_QUEUE_STORAGE_KEY = 'retroxide.download-queue';

/** How long "Download all" waits between files, so the browser keeps up. */
export const DOWNLOAD_GAP_MS = 1000;

type QueueStorage = Pick<Storage, 'getItem' | 'setItem'>;

function defaultStorage(): QueueStorage | null {
	try {
		return globalThis.sessionStorage ?? null;
	} catch {
		return null;
	}
}

const sleep = (milliseconds: number) => new Promise<void>((done) => setTimeout(done, milliseconds));

/**
 * The ROMs picked for downloading, kept for this tab across reloads. Each download is started by
 * the browser through a signed link, so the queue only tracks whether it started.
 */
export class DownloadQueue {
	items = $state<DownloadQueueItem[]>([]);

	readonly #download: (romId: string) => Promise<{ filename: string }>;
	readonly #storage: QueueStorage | null;
	readonly #wait: (milliseconds: number) => Promise<void>;
	readonly #describe: (error: unknown) => string;

	constructor(
		download: (romId: string) => Promise<{ filename: string }>,
		storage: QueueStorage | null = defaultStorage(),
		wait: (milliseconds: number) => Promise<void> = sleep,
		describe: (error: unknown) => string = (error) => (error instanceof Error ? error.message : String(error))
	) {
		this.#download = download;
		this.#storage = storage;
		this.#wait = wait;
		this.#describe = describe;
		this.items = this.#read();
	}

	has(romId: string): boolean {
		return this.items.some((item) => item.romId === romId);
	}

	/** Puts a ROM at the top of the queue; one already queued stays where it is. */
	add(rom: RomSummary): void {
		if (this.has(rom.rom_id)) return;
		this.#save([{ romId: rom.rom_id, title: rom.title, platform: rom.platform_name, status: 'ready', filename: null, error: null }, ...this.items]);
	}

	remove(romId: string): void {
		this.#save(this.items.filter((item) => item.romId !== romId));
	}

	clearCompleted(): void {
		this.#save(this.items.filter((item) => item.status !== 'completed'));
	}

	async start(romId: string): Promise<void> {
		this.#update(romId, { status: 'downloading', error: null });
		try {
			const { filename } = await this.#download(romId);
			this.#update(romId, { status: 'completed', filename, error: null });
		} catch (error) {
			this.#update(romId, { status: 'failed', error: this.#describe(error) });
		}
	}

	/** Starts every ready or failed item in turn, a moment apart; a failure does not stop the rest. */
	async startAll(): Promise<void> {
		const pending = this.items.filter((item) => item.status === 'ready' || item.status === 'failed').map((item) => item.romId);
		for (const [index, romId] of pending.entries()) {
			if (index > 0) await this.#wait(DOWNLOAD_GAP_MS);
			if (this.has(romId)) await this.start(romId);
		}
	}

	#update(romId: string, change: Partial<DownloadQueueItem>): void {
		this.#save(this.items.map((item) => (item.romId === romId ? { ...item, ...change } : item)));
	}

	#save(items: DownloadQueueItem[]): void {
		this.items = items;
		try {
			this.#storage?.setItem(DOWNLOAD_QUEUE_STORAGE_KEY, JSON.stringify(items));
		} catch {
			// Without storage the queue still works; it just does not survive a reload.
		}
	}

	#read(): DownloadQueueItem[] {
		try {
			const parsed: unknown = JSON.parse(this.#storage?.getItem(DOWNLOAD_QUEUE_STORAGE_KEY) ?? '[]');
			if (!Array.isArray(parsed)) return [];
			return parsed
				.filter((item): item is DownloadQueueItem => typeof item?.romId === 'string' && typeof item.title === 'string' && typeof item.platform === 'string' && typeof item.status === 'string')
				// A download cut off by a reload is not running any more.
				.map((item) => (item.status === 'downloading' ? { ...item, status: 'ready' as const } : item));
		} catch {
			return [];
		}
	}
}
