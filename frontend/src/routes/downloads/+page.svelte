<script lang="ts">
	import { browser } from '$app/env';
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import IconButton from '#lib/components/IconButton.svelte';
	import { downloadRom, i18n, listRoms } from '#lib';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import type { DownloadedRom, RomSummary } from '#lib';

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

	let roms = $state<RomSummary[]>([]);
	let queue = $state<DownloadQueueItem[]>([]);
	let loading = $state(false);
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
				platform: rom.platform,
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

	function triggerBrowserDownload(result: DownloadedRom): void {
		const url = URL.createObjectURL(result.blob);
		const anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = result.filename;
		anchor.rel = 'noopener';
		document.body.append(anchor);
		anchor.click();
		anchor.remove();
		requestAnimationFrame(() => URL.revokeObjectURL(url));
	}

	async function loadAllRoms(): Promise<RomSummary[]> {
		const items: RomSummary[] = [];
		let cursor: string | undefined;

		while (true) {
			const response = await listRoms(100, cursor);
			items.push(...response.item);

			if (!response.next_cursor) {
				return items;
			}

			cursor = response.next_cursor;
		}
	}

	async function loadRoms(): Promise<void> {
		loading = true;
		errorMessage = '';

		try {
			roms = await loadAllRoms();
		} catch (error) {
			errorMessage = getErrorMessage(error, 'downloads.catalog.error', 'Failed to load ROMs', t);
		} finally {
			loading = false;
		}
	}

	async function startDownload(romId: string): Promise<void> {
		updateQueueItem(romId, { status: 'downloading', error: null });
		statusMessage = '';

		try {
			const result = await downloadRom(romId);
			triggerBrowserDownload(result);
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
						<IconButton type="button" icon="/components/btns/Play - 01.png" activeIcon="/components/btns/Play - 02.png" active={item.status === 'downloading'} label={t('downloads.actions.start', 'Start download')} onclick={() => void startDownload(item.romId)} disabled={item.status === 'downloading'} />
						<IconButton type="button" icon="/components/btns/Cancel - 01.png" label={t('downloads.actions.remove', 'Remove')} onclick={() => removeQueueItem(item.romId)} />
					</div>
				</li>
			{/each}
		</ul>
	{:else if loading}
		<p class="page-state">{t('downloads.catalog.loading', 'Loading ROMs...')}</p>
	{:else}
		<ul class="download-list" aria-label={t('downloads.catalog.title', 'Available ROMs')}>
			{#each roms as rom (rom.rom_id)}
				<li class="download-row">
					<div><strong>{rom.title}</strong><span>{rom.platform}</span></div>
					<ActionButton type="button" onclick={() => addToQueue(rom)}>{t('downloads.actions.add', 'Add to queue')}</ActionButton>
				</li>
			{/each}
		</ul>
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

	.download-row > div:first-child { display: grid; gap: 0.5rem; }
	.download-row strong { font-size: 0.85rem; }
	.download-row span { color: var(--pixel-muted); font-size: 0.55rem; }
	.download-actions { display: flex; gap: 0.5rem; }
	.page-state { color: var(--pixel-muted); font-size: 0.6rem; }

	@media (max-width: 40rem) {
		h1 { font-size: 1rem; }
		.download-row { align-items: flex-start; gap: 1rem; }
		.download-row strong { font-size: 0.65rem; line-height: 1.5; }
	}
</style>