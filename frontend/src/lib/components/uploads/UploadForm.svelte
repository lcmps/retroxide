<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		uploading: boolean;
		/** Called whenever the picked file changes. */
		onselect?: (file: File | null) => void;
		onupload: (file: File) => void;
	};

	let { uploading, onselect, onupload }: Props = $props();

	const ACCEPTED_EXTENSIONS = '.rom,.bin,.nes,.sfc,.smc,.gba,.gb,.gbc,.n64,.z64,.v64,.iso,.cue,.chd';
	const t = useTranslate(i18n);
	let file = $state<File | null>(null);

	function select(event: Event & { currentTarget: HTMLInputElement }): void {
		file = event.currentTarget.files?.[0] ?? null;
		onselect?.(file);
	}

	function submit(event: SubmitEvent): void {
		event.preventDefault();
		if (file) {
			onupload(file);
		}
	}
</script>

<form class="upload-form" onsubmit={submit}>
	<label class="upload-picker">
		<span>{t('uploads.pick_file', 'Choose a ROM file')}</span>
		<input type="file" accept={ACCEPTED_EXTENSIONS} onchange={select} />
	</label>

	<p class="muted">
		{file ? t('uploads.form.selected', 'Selected file: {name}', { name: file.name }) : t('uploads.form.none', 'No file selected.')}
	</p>

	<ActionButton type="submit" disabled={uploading || !file}>
		{uploading ? t('common.status.uploading', 'Uploading...') : t('uploads.form.submit', 'Upload ROM')}
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
	}
</style>
