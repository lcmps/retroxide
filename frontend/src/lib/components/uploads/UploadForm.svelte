<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import type { BiosUploadOptions, Platform } from '#lib/api/types.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		/**
		 * Called with every picked file, and whether to compress discs to CHD; the picker is then
		 * cleared for the next batch.
		 */
		onupload: (files: File[], compressDiscs: boolean, bios?: BiosUploadOptions) => void;
		/** Whether discs can be compressed to CHD, and the initial choice. */
		compression?: { available: boolean; byDefault: boolean };
		/** The platforms BIOS files can be uploaded for. */
		biosPlatforms?: Platform[];
	};

	let { onupload, compression, biosPlatforms = [] }: Props = $props();
	/** Whether the picked files are BIOS files, for biosPlatform. */
	let biosMode = $state(false);
	let biosPlatform = $state('');
	let compressDiscs = $state(false);
	let touched = false;

	// Follow the server's default until the person chooses.
	$effect(() => {
		if (!touched) compressDiscs = compression?.byDefault ?? false;
	});


	// Mirrors the server's default allowlist (the platform catalog's extensions); the server
	// enforces its configured list either way.
	const ACCEPTED_EXTENSIONS = [
		'rom', 'bin', 'iso', 'cue', 'chd',
		'nes', 'fds', 'sfc', 'smc', 'gb', 'gbc', 'gba', 'n64', 'z64', 'v64', 'nds', '3ds', 'cia', 'vb', 'min',
		'gcm', 'rvz', 'wbfs', 'wua', 'nsp', 'xci',
		'sg', 'sms', 'gg', 'md', 'gen', 'smd', '32x', 'gdi', 'cdi', 'raw',
		'cso', 'pbp', 'vpk', 'pce', 'ngp', 'ngc', 'ws', 'wsc',
		'a26', 'a78', 'lnx', 'j64', 'col', 'int', 'vec'
	]
		.map((extension) => `.${extension}`)
		.join(',');
	const t = useTranslate(i18n);
	let files = $state<File[]>([]);
	let input = $state<HTMLInputElement>();
	const hasDisc = $derived(files.some((file) => /\.(cue|gdi)$/i.test(file.name)));

	const selection = $derived.by(() => {
		if (files.length === 0) return t('uploads.form.none', 'No file selected.');
		if (files.length === 1) return t('uploads.form.selected', 'Selected file: {name}', { name: files[0].name });
		return t('uploads.form.selectedMany', '{count} files selected', { count: files.length });
	});

	function select(event: Event & { currentTarget: HTMLInputElement }): void {
		files = Array.from(event.currentTarget.files ?? []);
	}

	function submit(event: SubmitEvent): void {
		event.preventDefault();
		if (files.length === 0) return;
		if (biosMode && !biosPlatform) return;
		onupload(files, compressDiscs && (compression?.available ?? false), biosMode ? { platform: biosPlatform } : undefined);
		files = [];
		if (input) input.value = '';
	}
</script>

<form class="upload-form" onsubmit={submit}>
	<label class="upload-picker">
		<span>{t('uploads.pick_file', 'Choose ROM files')}</span>
		<!-- BIOS files come in types beyond the game list; the server checks them against its list. -->
		<input bind:this={input} type="file" multiple accept={biosMode ? undefined : ACCEPTED_EXTENSIONS} onchange={select} />
	</label>

	<p class="muted">{selection}</p>

	{#if compression?.available}
		<label class="compress">
			<input type="checkbox" bind:checked={compressDiscs} onchange={() => (touched = true)} aria-describedby="compress-hint" />
			<span>{t('uploads.form.compress', 'Compress discs to CHD')}</span>
		</label>
		<p class="muted hint" id="compress-hint">
			{hasDisc
				? t('uploads.form.compressHint', 'For discs picked as a .cue with their track files. The uploaded files can still be downloaded exactly as they are. GDI discs are kept as uploaded.')
				: t('uploads.form.compressHintNoDisc', 'Applies to discs picked as a .cue with their track files.')}
		</p>
	{/if}

	<label class="compress">
		<input type="checkbox" bind:checked={biosMode} aria-describedby="bios-hint" />
		<span>{t('uploads.form.bios', 'These are BIOS files')}</span>
	</label>
	{#if biosMode}
		<label class="bios-platform">
			<span>{t('uploads.form.biosPlatform', 'Platform')}</span>
			<select bind:value={biosPlatform} required>
				<option value="" disabled>{t('uploads.form.biosPlatformPick', 'Choose a platform')}</option>
				{#each biosPlatforms as platform (platform.key)}
					<option value={platform.key}>{platform.name}</option>
				{/each}
			</select>
		</label>
	{/if}
	<p class="muted hint" id="bios-hint">
		{t('uploads.form.biosHint', 'BIOS files Retroxide recognizes are added as BIOS automatically. Use this for ones it does not, and pick their platform.')}
	</p>

	<ActionButton type="submit" disabled={files.length === 0 || (biosMode && !biosPlatform)}>
		{files.length > 1
			? t('uploads.form.submitMany', 'Upload {count} ROMs', { count: files.length })
			: t('uploads.form.submit', 'Upload ROM')}
	</ActionButton>
</form>

<style>
	.compress {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		font-size: 0.6rem;
		cursor: pointer;
	}

	.compress input {
		width: 1.1rem;
		height: 1.1rem;
		accent-color: var(--pixel-accent);
	}

	.bios-platform {
		display: grid;
		gap: 0.4rem;
		font-size: 0.6rem;
	}

	.bios-platform select {
		max-width: 100%;
		padding: 0.4rem;
		font: inherit;
		background: #202020;
		color: inherit;
		border: 2px solid var(--pixel-panel-strong);
	}

	.hint {
		margin-top: -0.5rem;
		font-size: 0.5rem;
		line-height: 1.7;
	}

	.upload-form,
	.upload-picker {
		display: grid;
		gap: 0.75rem;
		min-width: 0;
	}

	.upload-picker {
		padding: 1.25rem;
		border: 2px dashed var(--pixel-panel-strong);
		color: var(--pixel-muted);
		font-size: 0.65rem;
	}

	.upload-picker input[type='file'] {
		width: 100%;
		max-width: 100%;
		min-width: 0;
		font-size: 0.55rem;
		overflow: hidden;
	}

	.muted {
		margin: 0;
		color: var(--pixel-muted);
		font-size: 0.6rem;
		line-height: 1.6;
		overflow-wrap: anywhere;
	}

	@media (max-width: 640px) {
		.muted {
			text-align: center;
		}
	}
</style>
