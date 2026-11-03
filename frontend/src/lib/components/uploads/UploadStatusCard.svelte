<script lang="ts">
	import type { UploadState, UploadStatusResponse } from '#lib/api/types.js';
	import { getUploadProcessingErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	let { upload }: { upload: UploadStatusResponse } = $props();

	const t = useTranslate(i18n);
	const STATE_LABELS: Record<UploadState, [key: string, fallback: string]> = {
		processing: ['uploads.status.processing', 'Processing upload...'],
		completed: ['uploads.status.completed', 'Completed'],
		failed: ['uploads.status.failed', 'Failed']
	};
	const stateLabel = $derived(t(...STATE_LABELS[upload.status]));
</script>

<section class="status-card" aria-live="polite">
	<p>{t('uploads.messages.id', 'Upload ID: {uploadId}', { uploadId: upload.upload_id })}</p>
	<p>{t('uploads.messages.status', 'Status: {status}', { status: stateLabel })}</p>

	{#if upload.rom_id}
		<p>{t('uploads.messages.romId', 'ROM ID: {romId}', { romId: upload.rom_id })}</p>
	{/if}

	{#if upload.error}
		<p class="message">
			{t('uploads.messages.error', 'Error: {error}', { error: getUploadProcessingErrorMessage(upload.error, t) })}
		</p>
	{/if}
</section>

<style>
	.status-card {
		font-size: 0.55rem;
		line-height: 1.6;
		overflow-wrap: anywhere;
		display: grid;
		gap: 0.75rem;
		min-width: 0;
		padding: 1rem;
		border-left: 0.35rem solid var(--pixel-panel-strong);
		background: #202020;
	}

	.status-card p {
		margin: 0;
	}

	.message {
		margin: 0;
		padding: 0.75rem 0.85rem;
		border: 2px solid #e18a78;
		background: rgba(18, 22, 31, 0.92);
		color: #ffd8cf;
	}
</style>
