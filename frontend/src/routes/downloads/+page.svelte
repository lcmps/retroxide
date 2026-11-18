<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import IconButton from '#lib/components/IconButton.svelte';
	import LoadMoreSentinel from '#lib/components/LoadMoreSentinel.svelte';
	import { downloadRom, i18n } from '#lib';
	import { DownloadQueue, type DownloadStatus } from '#lib/downloads/queue.svelte.js';
	import { RomPager } from '#lib/roms/pager.svelte.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import type { RomSummary } from '#lib';

	const t = useTranslate(i18n);

	// The catalog loads page by page as the list scrolls instead of all at once.
	const catalog = new RomPager();
	// The browser streams each file to disk through a signed link and shows its own progress.
	const queue = new DownloadQueue(downloadRom, undefined, undefined, (error) =>
		getErrorMessage(error, 'common.errors.download', 'Failed to download ROM', t)
	);
	let errorMessage = $state('');
	let statusMessage = $state('');
	let downloadingAll = $state(false);

	const pending = $derived(queue.items.filter((item) => item.status === 'ready' || item.status === 'failed').length);
	const completed = $derived(queue.items.some((item) => item.status === 'completed'));

	function statusLabel(status: DownloadStatus): string {
		switch (status) {
			case 'downloading':
				return t('common.status.downloading', 'Downloading...');
			case 'completed':
				return t('common.status.completed', 'Completed');
			case 'failed':
				return t('common.status.failed', 'Failed');
			default:
				return t('downloads.status.ready', 'Ready to download');
		}
	}

	function addToQueue(rom: RomSummary): void {
		queue.add(rom);
		statusMessage = t('downloads.messages.added', '{title} added to the queue.', { title: rom.title });
		errorMessage = '';
	}

	async function startOne(romId: string): Promise<void> {
		statusMessage = '';
		await queue.start(romId);
		const item = queue.items.find((entry) => entry.romId === romId);
		if (item?.status === 'completed' && item.filename) {
			statusMessage = t('downloads.messages.downloaded', 'Downloaded {filename}.', { filename: item.filename });
		}
	}

	async function startAll(): Promise<void> {
		downloadingAll = true;
		statusMessage = '';
		try {
			await queue.startAll();
		} finally {
			downloadingAll = false;
		}
	}

	async function loadRoms(): Promise<void> {
		errorMessage = '';
		await catalog.reset();
		if (catalog.error) {
			errorMessage = getErrorMessage(catalog.error, 'downloads.catalog.error', 'Failed to load ROMs', t);
		}
	}

	onMount(() => {
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

	{#if queue.items.length > 0}
		<section class="download-section stack" aria-labelledby="download-queue-title">
			<div class="download-section__header">
				<h2 id="download-queue-title">{t('downloads.queue.title', 'Download Queue')}</h2>
				<div class="download-section__actions">
					{#if pending > 0}
						<ActionButton type="button" onclick={() => void startAll()} disabled={downloadingAll} aria-busy={downloadingAll} title={t('downloads.actions.allHint', 'Download every queued ROM, one after another')}>
							{t('downloads.actions.all', 'Download all')}
						</ActionButton>
					{/if}
					{#if completed}
						<ActionButton type="button" variant="secondary" onclick={() => queue.clearCompleted()}>
							{t('downloads.actions.clearCompleted', 'Clear completed')}
						</ActionButton>
					{/if}
				</div>
			</div>
			<ul class="download-list">
				{#each queue.items as item (item.romId)}
					<li class="download-row">
						<div>
							<strong>{item.title}</strong>
							<span>{item.platform}</span>
							<span class="download-status" class:download-status--failed={item.status === 'failed'}>
								{statusLabel(item.status)}{#if item.status === 'completed' && item.filename} · {item.filename}{/if}
							</span>
							{#if item.status === 'failed' && item.error}<span class="download-status download-status--failed" role="alert">{item.error}</span>{/if}
						</div>
						<div class="download-actions">
							<IconButton type="button" icon="/components/btns/Play - 01.png" activeIcon="/components/btns/Play - 02.png" active={item.status === 'downloading'} label={t('downloads.actions.startFor', 'Start downloading {title}', { title: item.title })} onclick={() => void startOne(item.romId)} disabled={item.status === 'downloading' || downloadingAll} />
							<IconButton type="button" icon="/components/btns/Cancel - 01.png" label={t('downloads.actions.removeFor', 'Remove {title} from the queue', { title: item.title })} onclick={() => queue.remove(item.romId)} disabled={item.status === 'downloading'} />
						</div>
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	<section class="download-section stack" aria-labelledby="download-catalog-title">
		<h2 id="download-catalog-title">{t('downloads.catalog.title', 'Available ROMs')}</h2>
		{#if catalog.loading && catalog.items.length === 0}
			<p class="page-state" role="status">{t('downloads.catalog.loading', 'Loading ROMs...')}</p>
		{:else}
			<ul class="download-list">
				{#each catalog.items as rom (rom.rom_id)}
					{@const queued = queue.has(rom.rom_id)}
					<li class="download-row">
						<div><strong>{rom.title}</strong><span>{rom.platform_name}</span></div>
						<ActionButton
							type="button"
							variant={queued ? 'secondary' : 'primary'}
							disabled={queued}
							onclick={() => addToQueue(rom)}
							aria-label={queued ? t('downloads.actions.queuedFor', '{title} is in the queue', { title: rom.title }) : t('downloads.actions.addFor', 'Add {title} to the download queue', { title: rom.title })}
							title={queued ? undefined : t('downloads.actions.addFor', 'Add {title} to the download queue', { title: rom.title })}
						>{queued ? t('downloads.actions.queued', 'Queued') : t('downloads.actions.add', 'Add to queue')}</ActionButton>
					</li>
				{/each}
			</ul>
			{#if !catalog.exhausted && !catalog.error}
				<LoadMoreSentinel onvisible={() => void catalog.loadMore()} key={catalog.items.length} />
			{/if}
		{/if}
	</section>

	{#if statusMessage}
		<p class="message" role="status">{statusMessage}</p>
	{/if}

	{#if errorMessage}
		<p class="message message--error" role="alert">{errorMessage}</p>
	{/if}
</section>

<style>
	h1 { margin: 0; font-size: 1.5rem; }
	h2 { margin: 0; font-size: 0.8rem; }

	.download-section { --layout-gap: 1.25rem; }
	.download-section__header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.75rem 1.5rem; }
	.download-section__actions { display: flex; flex-wrap: wrap; gap: 0.5rem; }
	.download-row span.download-status { color: var(--pixel-text); }
	.download-row span.download-status--failed { color: #e18a78; }

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
		.download-section__header { justify-content: center; text-align: center; }
		.download-row strong { font-size: 0.65rem; }
		.page-state, .message { text-align: center; }
	}
</style>