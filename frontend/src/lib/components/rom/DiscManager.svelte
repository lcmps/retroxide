<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Dialog } from 'bits-ui';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { getRom, listRoms, mergeRom, removeDisc, reorderDiscs, splitDisc } from '#lib/api/client.js';
	import { ApiClientError } from '#lib/api/errors.js';
	import type { RomDetail, RomDiscEntry, RomSummary } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { formatBytes } from '#lib/helpers/format.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		rom: RomDetail;
		onchanged: (rom: RomDetail) => void;
		/** Told when a disc change starts and ends, so the page can hold its other actions. */
		onbusy?: (busy: boolean) => void;
		/** True while the page is changing the ROM itself, such as scraping; disc actions wait. */
		locked?: boolean;
	};
	type Pending = { kind: 'remove'; disc: RomDiscEntry } | { kind: 'merge'; source: RomSummary };

	let { rom, onchanged, onbusy, locked = false }: Props = $props();

	const t = useTranslate(i18n);
	const id = $props.id();

	const SEARCH_DELAY_MS = 300;
	const SEARCH_LIMIT = 8;

	const discs = $derived(rom.discs ?? []);
	const several = $derived(discs.length > 1);

	/** True while any change runs, which disables every disc action. */
	let working = $state(false);
	const busy = $derived(working || locked);

	$effect(() => {
		onbusy?.(working);
		return () => onbusy?.(false);
	});
	let pending = $state<Pending | null>(null);
	let discError = $state('');
	let mergeError = $state('');

	let term = $state('');
	let results = $state<RomSummary[]>([]);
	let searching = $state(false);
	let searched = $state(false);
	let searchError = $state('');
	let searchRound = 0;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;

	// Nothing is fetched until a term is typed; a newer search makes an older answer stale.
	$effect(() => {
		const query = term.trim();
		clearTimeout(searchTimer);
		const round = ++searchRound;
		results = [];
		searched = false;
		searchError = '';
		searching = false;
		if (!query) return;
		searching = true;
		searchTimer = setTimeout(async () => {
			try {
				const response = await listRoms(SEARCH_LIMIT + 1, undefined, { q: query });
				if (round !== searchRound) return;
				results = response.item.filter((entry) => entry.rom_id !== rom.rom_id).slice(0, SEARCH_LIMIT);
				searched = true;
			} catch (error) {
				if (round !== searchRound) return;
				searchError = getValidationAwareErrorMessage(error, 'rom.manage.searchError', 'Failed to search the library', t);
			} finally {
				if (round === searchRound) searching = false;
			}
		}, SEARCH_DELAY_MS);
		return () => clearTimeout(searchTimer);
	});

	/** Runs one change with the other actions disabled; resolves with the ROM it returned, or null on failure. */
	async function run(
		action: () => Promise<RomDetail>,
		fail: (message: string, error: unknown) => void,
		fallbackKey: string,
		fallback: string
	): Promise<RomDetail | null> {
		working = true;
		discError = '';
		mergeError = '';
		try {
			return await action();
		} catch (error) {
			fail(getValidationAwareErrorMessage(error, fallbackKey, fallback, t), error);
			return null;
		} finally {
			working = false;
		}
	}

	async function move(index: number, by: -1 | 1): Promise<void> {
		const order = discs.map((entry) => entry.rom_file_id);
		[order[index], order[index + by]] = [order[index + by], order[index]];
		const updated = await run(() => reorderDiscs(rom.rom_id, order), (message) => (discError = message), 'rom.manage.reorderError', 'Failed to change the order of the discs');
		if (updated) onchanged(updated);
	}

	async function split(entry: RomDiscEntry): Promise<void> {
		const created = await run(() => splitDisc(rom.rom_id, entry.number), (message) => (discError = message), 'rom.manage.splitError', 'Failed to make the disc a separate title');
		if (created) await goto(resolve('/rom/[rom_id]', { rom_id: created.rom_id }));
	}

	/** A conflict means the discs were changed elsewhere; the list is refreshed so the next try uses what is there. */
	async function removeFailed(message: string, error: unknown): Promise<void> {
		if (!(error instanceof ApiClientError && error.status === 409)) {
			discError = message;
			return;
		}
		discError = t('rom.manage.discsChanged', 'The discs changed since you opened this page. Check the list and try again.');
		try {
			onchanged(await getRom(rom.rom_id));
		} catch {
			// The message already tells the user to check the list.
		}
	}

	async function confirm(): Promise<void> {
		const action = pending;
		if (!action || busy) return;
		const updated =
			action.kind === 'remove'
				? await run(() => removeDisc(rom.rom_id, action.disc.number, action.disc.rom_file_id), (message, error) => void removeFailed(message, error), 'rom.manage.removeError', 'Failed to remove the disc')
				: await run(() => mergeRom(rom.rom_id, action.source.rom_id), (message) => (mergeError = message), 'rom.manage.mergeError', 'Failed to merge the titles');
		pending = null;
		if (!updated) return;
		if (action.kind === 'merge') term = '';
		onchanged(updated);
	}

	function discCount(entry: RomSummary): string {
		const count = entry.disc_count ?? 1;
		return count === 1 ? t('rom.manage.oneDisc', '1 disc') : t('rom.manage.discCount', '{count} discs', { count });
	}
</script>

<section class="disc-manager" aria-labelledby="{id}-title">
	<h2 id="{id}-title">{t('rom.manage.title', 'Manage discs')}</h2>

	<ol class="disc-list">
		{#each discs as entry, index (entry.rom_file_id)}
			<li class="disc-row" aria-label={t('rom.discs.item', 'Disc {number}', { number: entry.number })}>
				<div class="disc-row__info">
					<strong>{t('rom.discs.item', 'Disc {number}', { number: entry.number })}</strong>
					<span class="disc-row__file">{entry.filename}</span>
					<span class="disc-row__size">{formatBytes(entry.size, i18n.locale)}</span>
				</div>
				<div class="disc-row__actions">
					<ActionButton variant="secondary" disabled={busy || index === 0} onclick={() => move(index, -1)}>{t('rom.manage.moveUp', 'Move up')}</ActionButton>
					<ActionButton variant="secondary" disabled={busy || index === discs.length - 1} onclick={() => move(index, 1)}>{t('rom.manage.moveDown', 'Move down')}</ActionButton>
					{#if several}
						<ActionButton variant="secondary" disabled={busy} onclick={() => split(entry)}>{t('rom.manage.split', 'Make separate title')}</ActionButton>
						<ActionButton variant="danger" disabled={busy} aria-haspopup="dialog" onclick={() => (pending = { kind: 'remove', disc: entry })}>{t('rom.manage.remove', 'Remove disc')}</ActionButton>
					{/if}
				</div>
			</li>
		{/each}
	</ol>
	{#if discError}<p class="disc-manager__error" role="alert">{discError}</p>{/if}

	<div class="disc-merge">
		<h3 id="{id}-merge">{t('rom.manage.mergeTitle', 'Merge another title into this one')}</h3>
		<label class="disc-merge__field">
			<span>{t('rom.manage.mergeLabel', 'Find a title')}</span>
			<input type="search" bind:value={term} disabled={busy} autocomplete="off" aria-describedby="{id}-merge-hint" />
		</label>
		<p class="hint" id="{id}-merge-hint">{t('rom.manage.mergeHint', 'Its discs are added to this title. Search by title.')}</p>
		<div role="status" aria-live="polite">
			{#if searching}
				<p class="hint">{t('rom.manage.searching', 'Searching...')}</p>
			{:else if searched && results.length === 0}
				<p class="hint">{t('rom.manage.mergeNone', 'No other titles found.')}</p>
			{/if}
		</div>
		{#if searchError}<p class="disc-manager__error" role="alert">{searchError}</p>{/if}
		{#if results.length}
			<ul class="disc-merge__results">
				{#each results as entry (entry.rom_id)}
					<li>
						<button type="button" class="disc-merge__result" disabled={busy} aria-haspopup="dialog" onclick={() => (pending = { kind: 'merge', source: entry })}>
							<span class="disc-merge__name">{entry.title}</span>
							<span class="disc-merge__meta">{entry.platform_name || entry.platform} · {discCount(entry)}</span>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
		{#if mergeError}<p class="disc-manager__error" role="alert">{mergeError}</p>{/if}
	</div>
</section>

<!-- The overlay and dialog classes are the ones Delete ROM uses, defined in the ROM page. -->
<Dialog.Root open={pending !== null} onOpenChange={(open) => { if (!open && !working) pending = null; }}>
	<Dialog.Portal>
		<Dialog.Overlay class="rom-delete-overlay" />
		<Dialog.Content class="rom-delete-dialog" onEscapeKeydown={(event) => { if (working) event.preventDefault(); }} onInteractOutside={(event) => { if (working) event.preventDefault(); }}>
			{#if pending?.kind === 'remove'}
				<Dialog.Title class="rom-delete-title">{t('rom.manage.removeTitle', 'Remove disc {number}?', { number: pending.disc.number })}</Dialog.Title>
				<Dialog.Description class="rom-delete-description">
					{t('rom.manage.removeDescription', 'Permanently delete the files of disc {number} ({filename})? Files still used by other ROMs will be kept. This cannot be undone.', { number: pending.disc.number, filename: pending.disc.filename })}
				</Dialog.Description>
			{:else if pending?.kind === 'merge'}
				<Dialog.Title class="rom-delete-title">{t('rom.manage.mergeConfirmTitle', 'Merge {source} into {title}?', { source: pending.source.title, title: rom.title })}</Dialog.Title>
				<Dialog.Description class="rom-delete-description">
					{t('rom.manage.mergeDescription', 'The discs of {source} are added to {title}, which keeps its own details. {source} is then deleted, with its details and cover. This cannot be undone.', { source: pending.source.title, title: rom.title })}
				</Dialog.Description>
			{/if}
			<div class="disc-manager__dialog-actions">
				<ActionButton variant="secondary" disabled={busy} onclick={() => (pending = null)}>{t('common.actions.cancel', 'Cancel')}</ActionButton>
				<ActionButton variant="danger" disabled={busy} aria-busy={working} onclick={confirm}>
					{#if pending?.kind === 'merge'}
						{working ? t('rom.manage.merging', 'Merging...') : t('rom.manage.mergeConfirm', 'Merge titles')}
					{:else}
						{working ? t('rom.manage.removing', 'Removing...') : t('rom.manage.removeConfirm', 'Remove permanently')}
					{/if}
				</ActionButton>
			</div>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>

<style>
	.disc-manager {
		display: grid;
		gap: 0.9rem;
		min-width: 0;
		padding-top: 1rem;
		border-top: 2px solid var(--pixel-border);
	}

	h2,
	h3 {
		margin: 0;
		font-size: 0.7rem;
		line-height: 1.5;
	}

	h3 {
		font-size: 0.6rem;
	}

	.disc-list,
	.disc-merge__results {
		display: grid;
		gap: 0.6rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.disc-row {
		display: grid;
		gap: 0.6rem;
		min-width: 0;
		padding: 0.7rem;
		border: 2px solid var(--pixel-border);
		border-radius: 0.35rem;
	}

	.disc-row__info {
		display: grid;
		gap: 0.2rem;
		min-width: 0;
		font-size: 0.55rem;
		line-height: 1.6;
	}

	.disc-row__file {
		overflow-wrap: anywhere;
	}

	.disc-row__size {
		color: var(--pixel-muted);
	}

	.disc-row__actions,
	.disc-manager__dialog-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
	}

	.disc-manager__dialog-actions {
		justify-content: flex-end;
		gap: 0.75rem;
	}

	.disc-merge {
		display: grid;
		gap: 0.6rem;
		min-width: 0;
	}

	.disc-merge__field {
		display: grid;
		gap: 0.4rem;
		min-width: 0;
	}

	.disc-merge__field span {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	input {
		width: 100%;
		min-width: 0;
		min-height: 2.4rem;
		padding: 0.5rem 0.75rem;
		border: 0;
		border-radius: 0.5rem;
		background: #262626;
		color: var(--pixel-text);
		font: inherit;
		font-size: 0.6rem;
	}

	input:focus-visible,
	.disc-merge__result:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.hint {
		margin: 0;
		font-size: 0.45rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.disc-merge__result {
		display: grid;
		gap: 0.2rem;
		width: 100%;
		padding: 0.6rem 0.75rem;
		border: 2px solid var(--pixel-border-strong);
		border-radius: 0.4rem;
		background: transparent;
		color: var(--pixel-text);
		font: inherit;
		text-align: start;
		cursor: pointer;
	}

	.disc-merge__result:hover:not(:disabled) {
		background: var(--pixel-panel-strong);
	}

	.disc-merge__result:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.disc-merge__name {
		font-size: 0.6rem;
		line-height: 1.5;
		overflow-wrap: anywhere;
	}

	.disc-merge__meta {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	.disc-manager__error {
		margin: 0;
		font-size: 0.5rem;
		line-height: 1.6;
		color: #e18a78;
		overflow-wrap: anywhere;
	}

	@media (max-width: 40rem) {
		.disc-row__actions :global(.action-button) {
			flex: 1 1 100%;
		}
	}
</style>
