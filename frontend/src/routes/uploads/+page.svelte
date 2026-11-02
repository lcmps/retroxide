<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import UploadForm from '#lib/components/uploads/UploadForm.svelte';
	import UploadQueueItem from '#lib/components/uploads/UploadQueueItem.svelte';
	import { uploadRom, uploadStatus } from '#lib/api/client.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import { UploadQueue } from '#lib/uploads/queue.svelte.js';

	const t = useTranslate(i18n);
	const queue = new UploadQueue(uploadRom, uploadStatus);

	const hasFinished = $derived(queue.items.some((item) => item.finished));

	onMount(() => {
		void i18n.initialize();
		return () => queue.stop();
	});
</script>

<section class="page-shell stack" aria-labelledby="uploads-title">
	<h1 id="uploads-title">{t('uploads.heading', 'Uploads')}</h1>

	<section class="upload-workspace stack" aria-label={t('uploads.heading', 'Uploads')}>
		<UploadForm onupload={(files) => queue.add(files)} />

		{#if queue.items.length > 0}
			<ul class="upload-queue stack" aria-label={t('uploads.queue.label', 'Upload queue')}>
				{#each queue.items as item (item.key)}
					<UploadQueueItem {item} />
				{/each}
			</ul>
			{#if hasFinished}
				<ActionButton type="button" onclick={() => queue.clearFinished()}>
					{t('uploads.queue.clearFinished', 'Clear finished')}
				</ActionButton>
			{/if}
		{:else}
			<p>{t('uploads.empty', 'No uploads in progress.')}</p>
		{/if}
	</section>
</section>

<style>
	h1 {
		margin: 0;
		font-size: 1.5rem;
	}

	.upload-workspace {
		width: min(100%, 38rem);
		padding-top: 1.5rem;
	}

	.upload-queue {
		margin: 0;
		padding: 0;
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}

		.upload-workspace {
			padding-top: 0.5rem;
		}
	}
</style>
