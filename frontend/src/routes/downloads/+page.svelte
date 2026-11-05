<script lang="ts">
	import { browser } from '$app/env';
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import IconButton from '#lib/components/IconButton.svelte';
	import LoadMoreSentinel from '#lib/components/LoadMoreSentinel.svelte';
	import { downloadRom, i18n } from '#lib';
	import { RomPager } from '#lib/roms/pager.svelte.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import type { RomSummary } from '#lib';

	type QueueItemStatus = 'ready' | 'downloading' | 'completed' | 'failed';

	type DownloadQueueItem = {
		romId: string;
		title: string;
		platform: string;
		status: QueueItemStatus;
		filename: string | null;
		error: string | null;
	};

	const DOWNLOAD_QUEUE_STORAGE_KEY = 'retroxide.download-queue';

	// The catalog loads page by page as the list scrolls instead of all at once.
	const catalog = new RomPager();
	let queue = $state<DownloadQueueItem[]>([]);
	let errorMessage = $state('');
	let statusMessage = $state('');

	const t = useTranslate(i18n);

	function getQueueStatusLabel(status: QueueItemStatus): string {
		if (status === 'downloading') {
			return t('common.status.downloading', 'Downloading...');
		}

		if (status === 'completed') {
			return t('common.status.completed', 'Completed');
		}

		if (status === 'failed') {
			return t('common.status.failed', 'Failed');
		}

		return t('downloads.status.ready', 'Ready to download');
	}

	function persistQueue(items: DownloadQueueItem[]): void {
		if (!browser) {
			return;
		}

		sessionStorage.setItem(DOWNLOAD_QUEUE_STORAGE_KEY, JSON.stringify(items));
	}

	function readQueue(): DownloadQueueItem[] {
		if (!browser) {
			return [];
		}

		const rawQueue = sessionStorage.getItem(DOWNLOAD_QUEUE_STORAGE_KEY);
		if (!rawQueue) {
			return [];
		}

		try {
			const parsed = JSON.parse(rawQueue) as DownloadQueueItem[];
			return Array.isArray(parsed)
				? parsed.filter(
						(item) =>
							typeof item.romId === 'string' &&
							typeof item.title === 'string' &&
							typeof item.platform === 'string' &&
							typeof item.status === 'string'
					)
				: [];
		} catch {
			return [];
		}
	}

	function updateQueueItem(romId: string, update: Partial<DownloadQueueItem>): void {
		queue = queue.map((item) => (item.romId === romId ? { ...item, ...update } : item));
		persistQueue(queue);
	}

	function removeQueueItem(romId: string): void {
		queue = queue.filter((item) => item.romId !== romId);
		persistQueue(queue);
	}

	function addToQueue(rom: RomSummary): void {
		if (queue.some((item) => item.romId === rom.rom_id)) {
			return;
		}

		queue = [
			{
				romId: rom.rom_id,
				title: rom.title,
				platform: rom.platform_name,
				status: 'ready',
				filename: null,
				error: null
			},
			...queue
		];
		persistQueue(queue);
		statusMessage = t('downloads.messages.added', '{title} added to the queue.', {
			title: rom.title
		});
		errorMessage = '';
	}


	async function loadRoms(): Promise<void> {
		errorMessage = '';
		await catalog.reset();
		if (catalog.error) {
			errorMessage = getErrorMessage(catalog.error, 'downloads.catalog.error', 'Failed to load ROMs', t);
		}
	}

	async function startDownload(romId: string): Promise<void> {
		updateQueueItem(romId, { status: 'downloading', error: null });
		statusMessage = '';

		try {
			// The browser streams the file to disk through a signed link and shows its own progress.
			const result = await downloadRom(romId);
			updateQueueItem(romId, {
				status: 'completed',
				filename: result.filename,
				error: null
			});
			statusMessage = t('downloads.messages.downloaded', 'Downloaded {filename}.', {
				filename: result.filename
			});
		} catch (error) {
			updateQueueItem(romId, {
				status: 'failed',
				error: getErrorMessage(error, 'common.errors.download', 'Failed to download ROM', t)
			});
		}
	}

	onMount(() => {
		queue = readQueue();
		void i18n.initialize().then(loadRoms).catch((error: unknown) => {
			errorMessage = getErrorMessage(error, 'downloads.catalog.error', 'Failed to load ROMs', t);
		});
	});
</script>

<svelte:head>
	<title>{t('downloads.heading', 'Downloads')} · Retroxide</title>
</svelte:head>

<section class="page-shell stack" aria-labelledby="downloads-title">
	<h1 id="downloads-title">{t('downloads.heading', 'Downloads')}</h1>

	{#if queue.length > 0}
		<ul class="download-list" aria-label={t('downloads.queue.title', 'Download Queue')}>
			{#each queue as item (item.romId)}
				<li class="download-row">
					<div>
						<strong>{item.title}</strong>
						<span>{item.platform}</span>
					</div>
					<div class="download-actions">
						<IconButton type="button" icon="/components/btns/Play - 01.png" activeIcon="/components/btns/Play - 02.png" active={item.status === 'downloading'} label={t('downloads.actions.startFor', 'Start downloading {title}', { title: item.title })} onclick={() => void startDownload(item.romId)} disabled={item.status === 'downloading'} />
						<IconButton type="button" icon="/components/btns/Cancel - 01.png" label={t('downloads.actions.removeFor', 'Remove {title} from the queue', { title: item.title })} onclick={() => removeQueueItem(item.romId)} />
					</div>
				</li>
			{/each}
		</ul>
	{:else if catalog.loading && catalog.items.length === 0}
		<p class="page-state" role="status">{t('downloads.catalog.loading', 'Loading ROMs...')}</p>
	{:else}
		<ul class="download-list" aria-label={t('downloads.catalog.title', 'Available ROMs')}>
			{#each catalog.items as rom (rom.rom_id)}
				<li class="download-row">
					<div><strong>{rom.title}</strong><span>{rom.platform_name}</span></div>
					<ActionButton
						type="button"
						onclick={() => addToQueue(rom)}
						aria-label={t('downloads.actions.addFor', 'Add {title} to the download queue', { title: rom.title })}
						title={t('downloads.actions.addFor', 'Add {title} to the download queue', { title: rom.title })}
					>{t('downloads.actions.add', 'Add to queue')}</ActionButton>
				</li>
			{/each}
		</ul>
		{#if !catalog.exhausted && !catalog.error}
			<LoadMoreSentinel onvisible={() => void catalog.loadMore()} key={catalog.items.length} />
		{/if}
	{/if}

	{#if statusMessage}
		<p class="message" role="status">{statusMessage}</p>
	{/if}

	{#if errorMessage}
		<p class="message message--error" role="alert">{errorMessage}</p>
	{/if}
</section>

<style>
	h1 { margin: 0; font-size: 1.5rem; }

	.download-list {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 1.5rem;
	}

	.download-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 2rem;
		min-height: 4rem;
	}

	/* The text takes what the actions leave; min-width lets a long title wrap instead of pushing them off the page. */
	.download-row > div:first-child { display: grid; gap: 0.5rem; min-width: 0; }
	.download-row strong { font-size: 0.85rem; line-height: 1.5; overflow-wrap: anywhere; }
	.download-row span { color: var(--pixel-muted); font-size: 0.55rem; line-height: 1.5; overflow-wrap: anywhere; }
	.download-row > :global(.action-button) { flex: none; }
	.download-actions { display: flex; flex: none; gap: 0.5rem; }
	.page-state { color: var(--pixel-muted); font-size: 0.6rem; }

	@media (max-width: 40rem) {
		h1 { font-size: 1rem; }
		.download-list { gap: 1.75rem; margin-top: 0.75rem; }
		.download-row { flex-direction: column; align-items: center; gap: 0.75rem; min-height: 0; text-align: center; }
		.download-row > div:first-child { justify-items: center; }
		.download-row strong { font-size: 0.65rem; }
		.page-state, .message { text-align: center; }
	}
</style>