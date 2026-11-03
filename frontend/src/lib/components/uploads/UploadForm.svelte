<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		/** Called with every picked file; the picker is then cleared for the next batch. */
		onupload: (files: File[]) => void;
	};

	let { onupload }: Props = $props();

	// Mirrors the server's default allowlist (the platform catalog's extensions); the server
	// enforces its configured list either way.
	const ACCEPTED_EXTENSIONS = [
		'rom', 'bin', 'iso', 'cue', 'chd',
		'nes', 'fds', 'sfc', 'smc', 'gb', 'gbc', 'gba', 'n64', 'z64', 'v64', 'nds', '3ds', 'cia', 'vb', 'min',
		'gcm', 'rvz', 'wbfs', 'wua', 'nsp', 'xci',
		'sg', 'sms', 'gg', 'md', 'gen', 'smd', '32x', 'gdi', 'cdi',
		'cso', 'pbp', 'vpk', 'pce', 'ngp', 'ngc', 'ws', 'wsc',
		'a26', 'a78', 'lnx', 'j64', 'col', 'int', 'vec'
	]
		.map((extension) => `.${extension}`)
		.join(',');
	const t = useTranslate(i18n);
	let files = $state<File[]>([]);
	let input = $state<HTMLInputElement>();

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
		onupload(files);
		files = [];
		if (input) input.value = '';
	}
</script>

<form class="upload-form" onsubmit={submit}>
	<label class="upload-picker">
		<span>{t('uploads.pick_file', 'Choose ROM files')}</span>
		<input bind:this={input} type="file" multiple accept={ACCEPTED_EXTENSIONS} onchange={select} />
	</label>

	<p class="muted">{selection}</p>

	<ActionButton type="submit" disabled={files.length === 0}>
		{files.length > 1
			? t('uploads.form.submitMany', 'Upload {count} ROMs', { count: files.length })
			: t('uploads.form.submit', 'Upload ROM')}
	</ActionButton>
</form>

<style>
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
