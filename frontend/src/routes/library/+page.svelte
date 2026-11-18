<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { Tooltip } from 'bits-ui';
	import LibrarySort from '#lib/components/LibrarySort.svelte';
	import BiosPanel from '#lib/components/rom/BiosPanel.svelte';
	import { startsOpen } from '#lib/bios/panel.js';
	import LoadMoreSentinel from '#lib/components/LoadMoreSentinel.svelte';
	import RomCover from '#lib/components/RomCover.svelte';
	import { i18n } from '#lib';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { platformBadge } from '#lib/platforms/badges.js';
	import { platforms } from '#lib/platforms/catalog.svelte.js';
	import { RomPager } from '#lib/roms/pager.svelte.js';
	import { applySortToParams, sortFromParams, type RomSort } from '#lib/roms/sort.js';
	import { onMount } from 'svelte';

	/** Wait for typing to pause before searching the server. */
	const SEARCH_DEBOUNCE_MS = 250;

	const pager = new RomPager();
	let ready = $state(false);
	let initErrorMessage = $state('');
	let loadedOnce = false;

	const t = useTranslate(i18n);

	const selectedPlatform = $derived(page.url.searchParams.get('platform')?.trim() ?? '');
	const searchQuery = $derived(page.url.searchParams.get('q')?.trim().toLowerCase() ?? '');
	const sort = $derived(sortFromParams(page.url.searchParams));
	const pageTitle = $derived(
		selectedPlatform
			? platforms.nameFor(selectedPlatform)
			: t('library.title', 'Library')
	);
	const filtered = $derived(Boolean(selectedPlatform || searchQuery));
	/** The library without search or platform, in the same order. */
	const clearFiltersHref = $derived.by(() => {
		const params = applySortToParams(new URLSearchParams(), sort);
		return params.size > 0 ? `/library?${params}` : '/library';
	});
	const countLabel = $derived.by(() => {
		const count = pager.total;
		if (count === null) return '';
		if (searchQuery) {
			return count === 1 ? t('library.results.one', '1 result') : t('library.results.other', '{count} results', { count });
		}
		return count === 1 ? t('library.count.one', '1 ROM') : t('library.count.other', '{count} ROMs', { count });
	});
	const loadErrorMessage = $derived(
		initErrorMessage || (pager.error ? getErrorMessage(pager.error, 'library.roms.error', 'Failed to load ROMs', t) : '')
	);

	// Filtering and search run on the server, one page at a time, so large catalogs load only
	// what is on screen. Filter changes restart from the first page.
	$effect(() => {
		const filters = {
			platform: selectedPlatform || undefined,
			q: searchQuery || undefined,
			sort: sort?.field,
			order: sort?.order
		};
		if (!ready) {
			return;
		}
		const timer = setTimeout(() => void pager.reset(filters), loadedOnce ? SEARCH_DEBOUNCE_MS : 0);
		loadedOnce = true;
		return () => clearTimeout(timer);
	});


	/** Keeps the order in the URL, beside the search and platform, so it survives reloads and links. */
	function changeSort(next: RomSort): void {
		const params = applySortToParams(page.url.searchParams, next);
		void goto(params.size > 0 ? `/library?${params.toString()}` : '/library', {
			replace: true,
			reset: false
		});
	}

	onMount(() => {
		void i18n
			.initialize()
			.catch((error: unknown) => {
				initErrorMessage = getErrorMessage(error, 'library.roms.error', 'Failed to load ROMs', t);
			})
			.finally(() => {
				ready = true;
			});
	});
</script>

<svelte:head>
	<title>{pageTitle} · Retroxide</title>
</svelte:head>

<section class="page-shell stack" aria-labelledby="library-title" style="--layout-gap: 2.75rem">
	<div class="library-heading">
		<div class="library-heading__title">
			<h1 id="library-title">{pageTitle}</h1>
			{#if countLabel && pager.items.length > 0}<p class="library-count" aria-live="polite">{countLabel}</p>{/if}
		</div>
		<LibrarySort {sort} onchange={changeSort} />
	</div>

	{#if selectedPlatform}
		<BiosPanel platform={selectedPlatform} initiallyOpen={startsOpen(page.url.searchParams)} />
	{/if}

	{#if pager.items.length === 0 && (pager.loading || !ready)}
		<p class="library-state" role="status">{t('library.roms.loading', 'Loading ROMs...')}</p>
	{:else if pager.items.length === 0 && !pager.error}
		<p class="library-state" role="status">
			{#if filtered}
				{t('library.roms.empty', 'No ROMs matched the current filters.')}
				<a href={clearFiltersHref}>{t('library.roms.clearFilters', 'Clear filters')}</a>
			{:else}
				{t('library.roms.emptyLibrary', 'Your library is empty.')}
				<a href={resolve('/uploads')}>{t('library.roms.uploadFirst', 'Upload ROMs')}</a>
			{/if}
		</p>
	{:else}
		<ul class="rom-gallery" aria-label={t('library.roms.title', 'ROM Catalog')} aria-busy={pager.loading}>
			{#each pager.items as rom (rom.rom_id)}
				{@const badge = platformBadge(rom.platform)}
				{@const discs = rom.disc_count > 1 ? t('library.discCount', '{count} discs', { count: rom.disc_count }) : ''}
				<li>
					<!-- The cover is hidden from screen readers, so the link names the platform the badge shows. -->
					<a class="rom-tile" href={resolve("/rom/[rom_id]", { rom_id: rom.rom_id })} aria-label={[rom.title, rom.platform_name, discs].filter(Boolean).join(", ")}>
						<div class="rom-cover" aria-hidden="true">
							<RomCover romId={rom.rom_id} coverArtUrl={rom.cover_art_url} coverThumbUrl={rom.cover_thumb_url} />
							{#if badge}<div class="rom-platform">{badge}</div>{/if}
							{#if discs}<div class="rom-discs">{discs}</div>{/if}
						</div>
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									<span {...props}>{rom.title}</span>
								{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Portal>
								<Tooltip.Content class="rom-tooltip" sideOffset={6}>
									<p class="rom-tooltip__title">{rom.title}</p>
									<p class="rom-tooltip__platform">{rom.platform_name}</p>
									{#if discs}<p class="rom-tooltip__platform">{discs}</p>{/if}
								</Tooltip.Content>
							</Tooltip.Portal>
						</Tooltip.Root>
					</a>
				</li>
			{/each}
		</ul>
		{#if !pager.exhausted && !pager.error}
			<LoadMoreSentinel onvisible={() => void pager.loadMore()} key={pager.items.length} />
		{/if}
	{/if}

  {#if loadErrorMessage}
    <p class="message message--error" role="alert">{loadErrorMessage}</p>
  {/if}
</section>

<style>
	h1 {
		margin: 0;
		font-size: 1.5rem;
		line-height: 1.35;
	}

	/* The title and the sort buttons share a row, the buttons at its end; narrow screens wrap them. */
	.library-heading {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem 1.5rem;
	}

	.library-heading__title {
		display: grid;
		gap: 0.4rem;
	}

	.library-count {
		margin: 0;
		color: var(--pixel-muted);
		font-size: 0.55rem;
	}

	.library-state a {
		margin-inline-start: 0.4rem;
		color: var(--pixel-text);
	}

	.library-state {
		margin: -1.75rem 0 0;
		font-size: 0.6rem;
		color: var(--pixel-muted);
	}

	.rom-gallery {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(7.75rem, 1fr));
		gap: 1.5rem;
		margin: -1.25rem 0 0;
		padding: 0;
		list-style: none;
		justify-items: start;
	}

	/* Tiles far off screen are not laid out or painted until they scroll near, so a long
	   library costs little more to keep than a short one. The intrinsic size keeps the
	   scrollbar honest for tiles that have not been laid out yet. */
	.rom-gallery > li {
		content-visibility: auto;
		contain-intrinsic-size: auto 7.75rem auto 13.5rem;
	}

	.rom-tile {
		display: grid;
		gap: 0.55rem;
		width: 7.75rem;
		color: var(--pixel-text);
		font-size: 0.5rem;
		line-height: 1.5;
	}

	.rom-cover {
		position: relative;
		width: 7.75rem;
		aspect-ratio: 25 / 36;
		background: #353535;
		border-radius: 0.5rem;
		overflow: hidden;
	}

	/* A floating plate; four hard shadows draw its border, leaving the corners cut like a pixel font's. */
	.rom-platform {
		position: absolute;
		left: 50%;
		bottom: 0.5625rem;
		transform: translateX(-50%);
		min-width: 2.875rem;
		padding: 0.3125rem 0.4375rem 0.25rem;
		background: var(--pixel-bg);
		box-shadow:
			0 -2px 0 var(--pixel-accent-strong),
			0 2px 0 var(--pixel-accent-strong),
			-2px 0 0 var(--pixel-accent-strong),
			2px 0 0 var(--pixel-accent-strong);
		color: #ffffff;
		font-size: 0.5rem;
		line-height: 1;
		text-align: center;
		white-space: nowrap;
	}

	/* Same plate as the platform's, at the top right so the two never meet. */
	.rom-discs {
		position: absolute;
		top: 0.5625rem;
		right: 0.5625rem;
		max-width: calc(100% - 1.125rem);
		padding: 0.3125rem 0.4375rem 0.25rem;
		background: var(--pixel-bg);
		box-shadow:
			0 -2px 0 var(--pixel-accent-strong),
			0 2px 0 var(--pixel-accent-strong),
			-2px 0 0 var(--pixel-accent-strong),
			2px 0 0 var(--pixel-accent-strong);
		color: #ffffff;
		font-size: 0.5rem;
		line-height: 1;
		white-space: nowrap;
	}

	.rom-tile:hover .rom-cover,
	.rom-tile:focus-visible .rom-cover {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 3px;
	}

	.rom-tile span {
		display: block;
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
	}

	:global(.rom-tooltip) {
		z-index: 30;
		max-width: 16rem;
		padding: 0.6rem 0.75rem;
		background: var(--pixel-panel-strong);
		border: 2px solid var(--pixel-border-strong);
		box-shadow: var(--pixel-shadow, 4px 4px 0 rgba(0, 0, 0, 0.4));
	}

	:global(.rom-tooltip__title) {
		margin: 0;
		font-size: 0.55rem;
		line-height: 1.5;
		color: var(--pixel-text);
		word-break: break-word;
	}

	:global(.rom-tooltip__platform) {
		margin: 0.4rem 0 0;
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	.message {
		margin: 0;
		font-size: 0.55rem;
		color: #e18a78;
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}

		.library-heading {
			justify-content: center;
			text-align: center;
		}

		.library-state {
			text-align: center;
		}
		/* Fixed-width columns centred as a group, instead of stretched tracks hugging the left. */
		.rom-gallery { grid-template-columns: repeat(auto-fill, 6.25rem); justify-content: center; gap: 1.25rem 1rem; }
		.rom-tile, .rom-cover { width: 6.25rem; }
		.rom-gallery > li { contain-intrinsic-size: auto 6.25rem auto 11rem; }
	}
</style>
