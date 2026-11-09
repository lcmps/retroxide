<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { deleteBios, downloadBios, labelBios, listBios } from '#lib/api/client.js';
	import type { Bios } from '#lib/api/types.js';
	import { collapsedSummary } from '#lib/bios/panel.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	/**
	 * A platform's BIOS files, above its games: one quiet line that opens into a list. It starts
	 * closed on every visit, since a BIOS is usually fetched once.
	 */
	let { platform }: { platform: string } = $props();

	const t = useTranslate(i18n);
	const LABEL_FIELDS = ['region', 'version', 'model', 'date', 'note'] as const;
	type LabelField = (typeof LABEL_FIELDS)[number];

	let entries = $state<Bios[]>([]);
	let open = $state(false);
	let message = $state('');
	/** The entry asking to confirm its deletion, and the one being labelled. */
	let confirming = $state<string | null>(null);
	let labelling = $state<string | null>(null);
	let draft = $state<Record<LabelField, string>>({ region: '', version: '', model: '', date: '', note: '' });

	const summary = $derived(collapsedSummary(entries));

	$effect(() => {
		const key = platform.toLowerCase();
		let cancelled = false;
		open = false;
		listBios(key)
			.then((loaded) => {
				if (!cancelled) entries = loaded;
			})
			.catch(() => {
				if (!cancelled) entries = [];
			});
		return () => {
			cancelled = true;
		};
	});

	async function run(action: () => Promise<void>): Promise<void> {
		message = '';
		try {
			await action();
		} catch (error) {
			message = getErrorMessage(error, 'library.bios.error', 'That did not work. Please try again.', t);
		}
	}

	function download(entry: Bios): Promise<void> {
		return run(async () => {
			await downloadBios(entry.bios_id);
		});
	}

	function remove(entry: Bios): Promise<void> {
		return run(async () => {
			await deleteBios(entry.bios_id);
			entries = entries.filter((other) => other.bios_id !== entry.bios_id);
			confirming = null;
		});
	}

	function startLabel(entry: Bios): void {
		labelling = entry.bios_id;
		draft = { region: entry.region ?? '', version: entry.version ?? '', model: entry.model ?? '', date: entry.date ?? '', note: entry.note ?? '' };
	}

	function saveLabel(event: SubmitEvent, entry: Bios): Promise<void> {
		event.preventDefault();
		return run(async () => {
			const updated = await labelBios(entry.bios_id, Object.fromEntries(LABEL_FIELDS.map((field) => [field, draft[field].trim() || null])));
			entries = entries.map((other) => (other.bios_id === updated.bios_id ? updated : other));
			labelling = null;
		});
	}

	const fieldLabels: Record<LabelField, [string, string]> = {
		region: ['library.bios.region', 'Region'],
		version: ['library.bios.version', 'Version'],
		model: ['library.bios.model', 'Model'],
		date: ['library.bios.date', 'Date'],
		note: ['library.bios.note', 'Note']
	};
</script>

{#if entries.length > 0}
	<section class="bios-panel" aria-label={t('library.bios.title', 'BIOS files')}>
		<button type="button" class="bios-toggle" aria-expanded={open} onclick={() => (open = !open)}>
			<span aria-hidden="true">{open ? '▾' : '▸'}</span>
			{t('library.bios.summary', 'BIOS · {count} files', { count: summary.count })}
			{#if summary.needsLabel > 0}
				<span class="bios-hint">{t('library.bios.needsLabel', '· {count} needs a label', { count: summary.needsLabel })}</span>
			{/if}
		</button>

		{#if open}
			<div class="bios-table-wrap">
				<table class="bios-table">
					<thead>
						<tr>
							<th>{t(...fieldLabels.region)}</th>
							<th>{t(...fieldLabels.version)}</th>
							<th>{t(...fieldLabels.model)}</th>
							<th>{t(...fieldLabels.date)}</th>
							<th><span class="visually-hidden">{t('library.bios.actions', 'Actions')}</span></th>
						</tr>
					</thead>
					<tbody>
						{#each entries as entry (entry.bios_id)}
							<tr>
								{#if !entry.recognized && !entry.region && !entry.version && !entry.model}
									<td colspan="4" class="bios-unknown">
										{entry.filename} <span class="bios-hint">{t('library.bios.unrecognized', '· not recognized')}</span>
									</td>
								{:else}
									<td>{entry.region ?? '—'}</td>
									<td>{entry.version ?? '—'}</td>
									<td>{entry.model ?? '—'}</td>
									<td>{entry.date ?? '—'}</td>
								{/if}
								<td class="bios-actions">
									<ActionButton variant="secondary" onclick={() => download(entry)} title={entry.expected_path}>
										{t('common.actions.download', 'Download')}
									</ActionButton>
									{#if entry.can_modify && !entry.recognized}
										<ActionButton variant="secondary" onclick={() => startLabel(entry)}>
											{t('library.bios.label', 'Label')}
										</ActionButton>
									{/if}
									{#if entry.can_modify}
										{#if confirming === entry.bios_id}
											<ActionButton variant="danger" onclick={() => remove(entry)}>
												{t('library.bios.confirmDelete', 'Delete it')}
											</ActionButton>
											<ActionButton variant="secondary" onclick={() => (confirming = null)}>
												{t('common.actions.cancel', 'Cancel')}
											</ActionButton>
										{:else}
											<ActionButton variant="danger" onclick={() => (confirming = entry.bios_id)}>
												{t('common.actions.delete', 'Delete')}
											</ActionButton>
										{/if}
									{/if}
								</td>
							</tr>
							{#if labelling === entry.bios_id}
								<tr>
									<td colspan="5">
										<form class="bios-label-form" onsubmit={(event) => saveLabel(event, entry)}>
											{#each LABEL_FIELDS as field (field)}
												<label>
													<span>{t(...fieldLabels[field])}</span>
													<input bind:value={draft[field]} maxlength={field === 'note' ? 255 : 64} />
												</label>
											{/each}
											<div class="bios-actions">
												<ActionButton type="submit">{t('library.bios.saveLabel', 'Save')}</ActionButton>
												<ActionButton variant="secondary" onclick={() => (labelling = null)}>
													{t('common.actions.cancel', 'Cancel')}
												</ActionButton>
											</div>
										</form>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
			{#if message}
				<p class="bios-message" role="alert">{message}</p>
			{/if}
		{/if}
	</section>
{/if}

<style>
	.bios-panel {
		border: 2px dashed var(--pixel-panel-strong);
		padding: 0.6rem 0.9rem;
		font-size: 0.6rem;
		min-width: 0;
	}

	.bios-toggle {
		all: unset;
		cursor: pointer;
		display: flex;
		gap: 0.5rem;
		align-items: baseline;
		width: 100%;
		color: var(--pixel-muted);
		font-size: 0.6rem;
	}

	.bios-toggle[aria-expanded='true'] {
		color: inherit;
	}

	.bios-toggle:focus-visible {
		outline: 2px solid var(--pixel-accent);
		outline-offset: 2px;
	}

	.bios-hint {
		color: var(--pixel-muted);
	}

	.bios-table-wrap {
		overflow-x: auto;
		margin-top: 0.75rem;
	}

	.bios-table {
		width: 100%;
		border-collapse: collapse;
	}

	.bios-table th {
		text-align: left;
		font-weight: normal;
		color: var(--pixel-muted);
		padding: 0.35rem 0.5rem;
		border-bottom: 2px solid var(--pixel-panel-strong);
	}

	.bios-table td {
		padding: 0.45rem 0.5rem;
		border-bottom: 1px solid #1d1d1d;
		vertical-align: middle;
	}

	.bios-unknown {
		overflow-wrap: anywhere;
	}

	.bios-actions {
		display: flex;
		gap: 0.4rem;
		flex-wrap: wrap;
		justify-content: flex-end;
	}

	.bios-label-form {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
		gap: 0.6rem;
		align-items: end;
	}

	.bios-label-form label {
		display: grid;
		gap: 0.3rem;
	}

	.bios-label-form input {
		font: inherit;
		padding: 0.35rem;
		background: #202020;
		color: inherit;
		border: 2px solid var(--pixel-panel-strong);
		min-width: 0;
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}

	.bios-message {
		margin: 0.6rem 0 0;
		color: #ffd8cf;
	}
</style>
