<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { Tooltip } from 'bits-ui';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import { addRomToLibrary, createLibrary, fetchRomCover, getLibrary, i18n, listLibraries, listRoms, removeRomFromLibrary } from '$lib';
	import type { LibraryDetail, LibrarySummary, RomSummary } from '$lib';
	import { useTranslate } from '$lib/i18n/helpers';
	import { getErrorMessage } from '$lib/helpers/error';
	import { onMount } from 'svelte';

	let roms = $state<RomSummary[]>([]);
	let libraries = $state<LibrarySummary[]>([]);
	let selectedLibraryId = $state('');
	let selectedLibrary = $state<LibraryDetail | null>(null);
	let newLibraryName = $state('');
	let loading = $state(false);
	let librariesLoading = $state(false);
	let creatingLibrary = $state(false);
	let loadErrorMessage = $state('');
	let libraryErrorMessage = $state('');
	let membershipMessage = $state('');
	let membershipBusy = $state<Record<string, boolean>>({});
	let coverPreviews = $state<Record<string, string>>({});

	const t = useTranslate(i18n);

	const selectedPlatform = $derived(page.url.searchParams.get('platform')?.trim() ?? '');
	const searchQuery = $derived(page.url.searchParams.get('q')?.trim().toLowerCase() ?? '');
	const libraryTitle = $derived(
		selectedPlatform
			? formatPlatformName(selectedPlatform)
			: t('library.title', 'Library')
	);
	const filteredRoms = $derived.by(() =>
		roms.filter((rom) => {
			const matchesPlatform = !selectedPlatform || rom.platform === selectedPlatform;
			const matchesQuery =
				!searchQuery ||
				rom.title.toLowerCase().includes(searchQuery) ||
				rom.platform.toLowerCase().includes(searchQuery) ||
				rom.rom_id.toLowerCase().includes(searchQuery);

			return matchesPlatform && matchesQuery;
		})
	);
	const selectedROMIDs = $derived(new Set(selectedLibrary?.rom_ids ?? []));

	function formatPlatformName(platform: string): string {
		const labels: Record<string, string> = {
			nes: 'NES',
			snes: 'SNES',
			gba: 'Game Boy Advance',
			gb: 'Game Boy',
			gbc: 'Game Boy Color',
			n64: 'Nintendo 64',
			psx: 'PlayStation'
		};

		return labels[platform.toLowerCase()] ?? platform;
	}

	async function loadRoms() {
		loading = true;
		loadErrorMessage = '';

		try {
			roms = await loadAllRoms();
			await loadCoverPreviews(roms);
		} catch (error) {
			loadErrorMessage = getErrorMessage(error, 'library.roms.error', 'Failed to load ROMs', t);
		} finally {
			loading = false;
		}
	}

	async function loadLibraries() {
		librariesLoading = true;
		libraryErrorMessage = '';
		try {
			libraries = await listLibraries();
			if (!libraries.some((library) => library.library_id === selectedLibraryId)) {
				selectedLibraryId = libraries[0]?.library_id ?? '';
			}
			await loadSelectedLibrary();
		} catch (error) {
			libraryErrorMessage = getErrorMessage(error, 'library.collections.loadFailure', 'Failed to load collections', t);
		} finally {
			librariesLoading = false;
		}
	}

	async function loadSelectedLibrary() {
		selectedLibrary = selectedLibraryId ? await getLibrary(selectedLibraryId) : null;
	}

	async function selectLibrary(event: Event) {
		selectedLibraryId = (event.currentTarget as HTMLSelectElement).value;
		membershipMessage = '';
		libraryErrorMessage = '';
		try {
			await loadSelectedLibrary();
		} catch (error) {
			libraryErrorMessage = getErrorMessage(error, 'library.collections.loadFailure', 'Failed to load collection', t);
		}
	}

	async function submitLibrary(event: SubmitEvent) {
		event.preventDefault();
		creatingLibrary = true;
		libraryErrorMessage = '';
		try {
			const created = await createLibrary({ name: newLibraryName });
			newLibraryName = '';
			libraries = await listLibraries();
			selectedLibraryId = created.library_id;
			await loadSelectedLibrary();
		} catch (error) {
			libraryErrorMessage = getErrorMessage(error, 'library.collections.createFailure', 'Failed to create collection', t);
		} finally {
			creatingLibrary = false;
		}
	}

	async function toggleROMMembership(rom: RomSummary) {
		if (!selectedLibrary || membershipBusy[rom.rom_id]) return;
		membershipBusy = { ...membershipBusy, [rom.rom_id]: true };
		libraryErrorMessage = '';
		membershipMessage = '';
		try {
			if (selectedROMIDs.has(rom.rom_id)) {
				await removeRomFromLibrary(selectedLibrary.library_id, rom.rom_id);
				membershipMessage = t('library.collections.removed', 'Removed from {name}.', { name: selectedLibrary.name });
			} else {
				await addRomToLibrary(selectedLibrary.library_id, rom.rom_id);
				membershipMessage = t('library.collections.added', 'Added to {name}.', { name: selectedLibrary.name });
			}
			await loadSelectedLibrary();
		} catch (error) {
			libraryErrorMessage = getErrorMessage(error, 'library.collections.updateFailure', 'Failed to update collection membership', t);
		} finally {
			const remaining = { ...membershipBusy };
			delete remaining[rom.rom_id];
			membershipBusy = remaining;
		}
	}

	async function loadCoverPreviews(items: RomSummary[]): Promise<void> {
		for (const url of Object.values(coverPreviews)) URL.revokeObjectURL(url);
		const previews = await Promise.allSettled(items.filter((rom) => rom.cover_art_url).map(async (rom) => [rom.rom_id, URL.createObjectURL(await fetchRomCover(rom.rom_id))] as const));
		const successful = previews.flatMap((result) => result.status === 'fulfilled' ? [result.value] : []);
		coverPreviews = Object.fromEntries(successful);
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

	onMount(() => {
		void i18n.initialize().then(async () => {
			await Promise.all([loadLibraries(), loadRoms()]);
		}).catch((error: unknown) => {
			loadErrorMessage = getErrorMessage(error, 'library.roms.error', 'Failed to load ROMs', t);
		});
	});
</script>

<section class="page-shell stack" aria-labelledby="library-title" style="--layout-gap: 2.75rem">
	<div class="library-heading">
		<h1 id="library-title">{libraryTitle}</h1>
	</div>

	<section class="collection-tools stack" aria-label={t('library.collections.title', 'Collections')}>
		<div class="collection-select">
			<label for="library-select">{t('library.collections.select', 'Collection')}</label>
			<select id="library-select" value={selectedLibraryId} onchange={selectLibrary} disabled={librariesLoading || libraries.length === 0}>
				<option value="">{t('library.collections.none', 'No collection selected')}</option>
				{#each libraries as library (library.library_id)}
					<option value={library.library_id}>{library.name}</option>
				{/each}
			</select>
			{#if selectedLibrary}
				<p>{t('library.collections.memberCount', '{count} ROMs in this collection', { count: selectedLibrary.rom_ids.length })}</p>
			{/if}
		</div>
		<form class="collection-create" onsubmit={submitLibrary}>
			<label for="new-library-name">{t('library.collections.createLabel', 'New collection')}</label>
			<div class="collection-create__controls">
				<input id="new-library-name" bind:value={newLibraryName} maxlength="255" required placeholder={t('library.form.namePlaceholder', 'My SNES Collection')} />
				<ActionButton type="submit" disabled={creatingLibrary || !newLibraryName.trim()}>{creatingLibrary ? t('library.form.submitting', 'Creating...') : t('library.form.submit', 'Create Collection')}</ActionButton>
			</div>
		</form>
	</section>
	{#if membershipMessage}<p class="message" role="status">{membershipMessage}</p>{/if}
	{#if libraryErrorMessage}<p class="message message--error" role="alert">{libraryErrorMessage}</p>{/if}

	{#if loading}
		<p class="library-state">{t('library.roms.loading', 'Loading ROMs...')}</p>
	{:else if filteredRoms.length === 0}
		<p class="library-state">{t('library.roms.empty', 'No ROMs matched the current filters.')}</p>
	{:else}
		<ul class="rom-gallery grid grid--auto" aria-label={t('library.roms.title', 'ROM Catalog')} style="--grid-min: 6rem; --layout-gap: 1.5rem">
			{#each filteredRoms as rom (rom.rom_id)}
				<li class="rom-entry">
					<a class="rom-tile" href={resolve(`/rom/${rom.rom_id}`)} aria-label={rom.title}>
						<div class="rom-cover" aria-hidden="true">{#if coverPreviews[rom.rom_id]}<img src={coverPreviews[rom.rom_id]} alt="" />{/if}</div>
						<Tooltip.Root>
							<Tooltip.Trigger>
								{#snippet child({ props })}
									<span {...props}>{rom.title}</span>
								{/snippet}
							</Tooltip.Trigger>
							<Tooltip.Portal>
								<Tooltip.Content class="rom-tooltip" sideOffset={6}>
									<p class="rom-tooltip__title">{rom.title}</p>
									<p class="rom-tooltip__platform">{formatPlatformName(rom.platform)}</p>
								</Tooltip.Content>
							</Tooltip.Portal>
						</Tooltip.Root>
					</a>
					<ActionButton class="membership-action" type="button" variant="secondary" disabled={!selectedLibrary || membershipBusy[rom.rom_id]} onclick={() => void toggleROMMembership(rom)}>
						{selectedROMIDs.has(rom.rom_id) ? t('library.collections.remove', 'Remove') : t('library.collections.add', 'Add')}
					</ActionButton>
				</li>
			{/each}
		</ul>
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

	.library-state {
		margin: -1.75rem 0 0;
		font-size: 0.6rem;
		color: var(--pixel-muted);
	}

	.rom-gallery {
		margin: -1.25rem 0 0;
		padding: 0;
		list-style: none;
		justify-items: center;
	}

	.collection-tools {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 15rem), 1fr));
		align-items: end;
		gap: 1rem;
		max-width: 48rem;
		padding: 1rem;
		border: 2px solid var(--pixel-panel-strong);
		background: var(--pixel-panel);
	}

	.collection-select, .collection-create { display: grid; gap: .55rem; min-width: 0; }
	.collection-select label, .collection-create label { font-size: .55rem; color: var(--pixel-muted); }
	.collection-select select, .collection-create input {
		min-width: 0; width: 100%; min-height: 2.5rem; padding: .6rem .7rem;
		border: 2px solid var(--pixel-border-strong); background: var(--pixel-bg); color: var(--pixel-text);
		font: inherit; font-size: .55rem;
	}
	.collection-select p { margin: 0; color: var(--pixel-muted); font-size: .5rem; }
	.collection-create__controls { display: flex; align-items: center; gap: .5rem; min-width: 0; }
	.collection-create__controls input { flex: 1; }
	.rom-entry { display: grid; justify-items: center; gap: .5rem; }
	:global(.membership-action) { min-width: 5.5rem; padding: .5rem; font-size: .45rem; }

	.rom-tile {
		display: grid;
		gap: 0.55rem;
		width: 6rem;
		color: var(--pixel-text);
		font-size: 0.5rem;
		line-height: 1.5;
	}

	.rom-cover {
		width: 6rem;
		aspect-ratio: 2 / 3;
		background: #353535;
		border-radius: 0.5rem;
		overflow: hidden;
	}

	.rom-cover img { display: block; width: 100%; height: 100%; object-fit: cover; }

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
		color: var(--pixel-danger);
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}
	}
</style>
