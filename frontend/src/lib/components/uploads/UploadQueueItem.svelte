<script lang="ts">
	import UploadProgress from '#lib/components/uploads/UploadProgress.svelte';
	import UploadStatusCard from '#lib/components/uploads/UploadStatusCard.svelte';
	import { getErrorMessage, getUploadStatusRetryMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import type { UploadItem } from '#lib/uploads/queue.svelte.js';

	let { item }: { item: UploadItem } = $props();

	const t = useTranslate(i18n);
</script>

<li class="queue-item stack">
	<p class="file-name" title={item.file.name}>{item.file.name}</p>

	{#if item.phase === 'queued'}
		<p class="muted">{t('uploads.queue.waiting', 'Waiting to upload...')}</p>
	{:else if item.phase === 'uploading'}
		<UploadProgress percent={item.progress} />
	{:else if item.phase === 'rejected'}
		<p class="message message--error" role="alert">
			{getErrorMessage(item.error, 'common.errors.upload', 'Failed to upload ROM', t)}
		</p>
	{:else if item.tracker.status}
		<UploadStatusCard upload={item.tracker.status} />
		{#if item.tracker.failing}
			<p class="message message--error" role="alert">{getUploadStatusRetryMessage(t)}</p>
		{/if}
	{/if}
</li>

<style>
	.queue-item {
		min-width: 0;
		padding: 1rem;
		border: 2px solid var(--pixel-border);
		list-style: none;
	}

	.file-name {
		margin: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.muted {
		margin: 0;
		color: var(--pixel-muted);
	}

	.message {
		margin: 0;
		padding: 0.75rem 0.85rem;
		border: 2px solid #e18a78;
		background: rgba(18, 22, 31, 0.92);
		color: #ffd8cf;
	}
</style>
