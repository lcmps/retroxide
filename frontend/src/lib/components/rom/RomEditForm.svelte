<script lang="ts">
	import { onMount, tick } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { listPlatformCatalog, updateRom } from '#lib/api/client.js';
	import type { Platform, RomDetail } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		rom: RomDetail;
		onsaved: (rom: RomDetail) => void;
		oncancel: () => void;
	};

	let { rom, onsaved, oncancel }: Props = $props();

	const t = useTranslate(i18n);
	const id = $props.id();

	// The form edits a copy of the ROM as it was when editing started.
	// svelte-ignore state_referenced_locally
	let values = $state({
		title: rom.title,
		platform: rom.platform,
		region: rom.region ?? '',
		version: rom.version ?? '',
		publisher: rom.publisher ?? '',
		release_year: rom.release_year ?? '',
		genre: rom.genre ?? '',
		description: rom.description ?? ''
	});
	let catalog = $state<Platform[]>([]);
	let saving = $state(false);
	let error = $state('');
	let titleInput = $state<HTMLInputElement>();
	let form = $state<HTMLFormElement>();

	// Escape anywhere in the form cancels editing, like closing a dialog.
	$effect(() => {
		form?.addEventListener('keydown', cancelOnEscape);
		return () => form?.removeEventListener('keydown', cancelOnEscape);
	});

	// A platform outside the catalog, such as one named by a metadata source, stays selectable.
	const platformOptions = $derived.by(() => {
		const options = catalog.map((platform) => ({ value: platform.key, label: platform.name }));
		if (!options.some((option) => option.value.toLowerCase() === rom.platform.toLowerCase())) {
			options.unshift({ value: rom.platform, label: rom.platform_name || rom.platform });
		}
		return options;
	});
	const titleMissing = $derived(values.title.trim() === '');

	onMount(() => {
		void tick().then(() => titleInput?.focus());
		listPlatformCatalog()
			.then((platforms) => (catalog = platforms))
			.catch(() => undefined);
	});

	async function save(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (titleMissing || saving) return;
		saving = true;
		error = '';
		try {
			onsaved(
				await updateRom(rom.rom_id, {
					title: values.title.trim(),
					platform: values.platform,
					region: values.region.trim() || null,
					version: values.version.trim() || null,
					publisher: values.publisher,
					release_year: values.release_year,
					genre: values.genre,
					description: values.description
				})
			);
		} catch (caught) {
			error = getValidationAwareErrorMessage(caught, 'rom.edit.error', 'Failed to save the changes', t);
		} finally {
			saving = false;
		}
	}

	function cancelOnEscape(event: KeyboardEvent): void {
		if (event.key === 'Escape' && !saving) {
			event.preventDefault();
			oncancel();
		}
	}
</script>

<form bind:this={form} class="rom-edit stack" onsubmit={save} aria-labelledby={`${id}-title`}>
	<h2 id={`${id}-title`}>{t('rom.edit.title', 'Edit details')}</h2>

	<label class="field">
		<span>{t('rom.fields.title', 'Title')}</span>
		<input bind:this={titleInput} bind:value={values.title} required maxlength="255" aria-invalid={titleMissing} />
	</label>

	<label class="field">
		<span>{t('rom.fields.platform', 'Platform')}</span>
		<select bind:value={values.platform}>
			{#each platformOptions as option (option.value)}
				<option value={option.value}>{option.label}</option>
			{/each}
		</select>
	</label>

	<div class="field-row">
		<label class="field">
			<span>{t('rom.fields.region', 'Region')}</span>
			<input bind:value={values.region} maxlength="64" />
		</label>
		<label class="field">
			<span>{t('rom.fields.version', 'Version')}</span>
			<input bind:value={values.version} maxlength="64" />
		</label>
	</div>

	<div class="field-row">
		<label class="field">
			<span>{t('rom.fields.publisher', 'Publisher')}</span>
			<input bind:value={values.publisher} maxlength="255" />
		</label>
		<label class="field">
			<span>{t('rom.fields.release_year', 'Release year')}</span>
			<input bind:value={values.release_year} maxlength="16" inputmode="numeric" />
		</label>
	</div>

	<label class="field">
		<span>{t('rom.fields.genre', 'Genre')}</span>
		<input bind:value={values.genre} maxlength="64" placeholder={t('rom.edit.genrePlaceholder', 'Action, Platform, Puzzle...')} />
	</label>

	<label class="field">
		<span>{t('rom.fields.description', 'Description')}</span>
		<textarea bind:value={values.description} rows="5" maxlength="4000"></textarea>
	</label>

	<p class="hint">{t('rom.edit.hint', 'Leave a field empty to clear it. Edited titles and platforms are kept when the ROM is scraped again.')}</p>

	{#if error}
		<p class="message message--error" role="alert">{error}</p>
	{/if}

	<div class="actions">
		<ActionButton type="submit" disabled={saving || titleMissing} aria-busy={saving}>
			{saving ? t('common.status.saving', 'Saving...') : t('rom.edit.save', 'Save changes')}
		</ActionButton>
		<ActionButton type="button" variant="secondary" class="cancel" disabled={saving} onclick={oncancel}>
			{t('common.actions.cancel', 'Cancel')}
		</ActionButton>
	</div>
</form>

<style>
	.rom-edit {
		--layout-gap: 0.9rem;
		min-width: 0;
	}

	h2 {
		margin: 0;
		font-size: 0.7rem;
	}

	.field {
		display: grid;
		gap: 0.4rem;
		min-width: 0;
	}

	.field span {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	.field-row {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 10rem), 1fr));
		gap: 0.9rem;
	}

	input,
	select,
	textarea {
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
		line-height: 1.6;
	}

	textarea {
		resize: vertical;
	}

	input:focus-visible,
	select:focus-visible,
	textarea:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.hint {
		margin: 0;
		font-size: 0.45rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.message {
		margin: 0;
		font-size: 0.55rem;
		line-height: 1.6;
	}

	.message--error {
		color: #e18a78;
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
	}

	/* A visible outline so the secondary button reads as a button. */
	.actions :global(.cancel) {
		box-shadow: inset 0 0 0 2px var(--pixel-border);
	}
</style>
