<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import UploadForm from '#lib/components/uploads/UploadForm.svelte';
	import UploadQueueItem from '#lib/components/uploads/UploadQueueItem.svelte';
	import { uploadStatus } from '#lib/api/client.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import { UploadQueue } from '#lib/uploads/queue.svelte.js';
	import { serverApi, uploadResumable } from '#lib/uploads/resumable.js';

	const t = useTranslate(i18n);
	// Large files go in parts that survive a dropped connection and resume when added again.
	const queue = new UploadQueue(
		(file, onProgress, onReconnecting) => uploadResumable(file, serverApi, { onProgress, onReconnecting }),
		uploadStatus
	);

	const hasFinished = $derived(queue.items.some((item) => item.finished));

	onMount(() => {
		void i18n.initialize();
		return () => queue.stop();
	});
</script>

<svelte:head>
	<title>{t('uploads.heading', 'Uploads')} · Retroxide</title>
</svelte:head>

<section class="page-shell" aria-labelledby="uploads-title">
	<!-- Centred as a block, like the other pages; the text inside stays left-aligned. -->
	<div class="upload-layout stack">
		<h1 id="uploads-title">{t('uploads.heading', 'Uploads')}</h1>

		<div class="upload-workspace stack">
			<UploadForm onupload={(files) => queue.add(files)} />

			{#if queue.items.length > 0}
				<ul class="upload-queue stack" aria-label={t('uploads.queue.label', 'Upload queue')}>
					{#each queue.items as item (item.key)}
						<UploadQueueItem {item} />
					{/each}
				</ul>
				{#if hasFinished}
					<ActionButton type="button" onclick={() => queue.clearFinished()} title={t('uploads.queue.clearHint', 'Remove completed and failed uploads from this list')}>
						{t('uploads.queue.clearFinished', 'Clear finished')}
					</ActionButton>
				{/if}
			{:else}
				<p>{t('uploads.empty', 'No uploads in progress.')}</p>
			{/if}
		</div>
	</div>
</section>

<style>
	h1 {
		margin: 0;
		font-size: 1.5rem;
	}

	.upload-layout {
		width: 100%;
		max-width: 38rem;
		margin-inline: auto;
	}

	.upload-workspace {
		padding-top: 1.5rem;
	}

	.upload-queue {
		margin: 0;
		padding: 0;
	}

	.upload-workspace > p {
		margin: 0;
		color: var(--pixel-muted);
		font-size: 0.6rem;
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}

		.upload-workspace {
			padding-top: 0.5rem;
		}

		.upload-workspace > p {
			text-align: center;
		}
	}
</style>
