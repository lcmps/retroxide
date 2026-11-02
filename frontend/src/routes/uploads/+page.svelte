<script lang="ts">
	import { onMount } from 'svelte';
	import UploadForm from '#lib/components/uploads/UploadForm.svelte';
	import UploadProgress from '#lib/components/uploads/UploadProgress.svelte';
	import UploadStatusCard from '#lib/components/uploads/UploadStatusCard.svelte';
	import { uploadRom, uploadStatus } from '#lib/api/client.js';
	import { getErrorMessage, getUploadStatusRetryMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import { UploadTracker } from '#lib/uploads/tracker.svelte.js';

	const t = useTranslate(i18n);
	const tracker = new UploadTracker(uploadStatus);

	let uploading = $state(false);
	let progress = $state(0);
	let uploadError = $state('');

	const errorMessage = $derived(uploadError || (tracker.failing ? getUploadStatusRetryMessage(t) : ''));
	const statusMessage = $derived(
		tracker.status && !tracker.failing
			? t('uploads.messages.accepted', 'Upload accepted. Tracking {uploadId}.', { uploadId: tracker.status.upload_id })
			: ''
	);

	function resetForNewFile(): void {
		progress = 0;
		uploadError = '';
	}

	async function upload(file: File): Promise<void> {
		tracker.reset();
		uploading = true;
		progress = 0;
		uploadError = '';
		try {
			const accepted = await uploadRom(file, (loaded, total) => {
				progress = total > 0 ? Math.round((loaded / total) * 100) : 0;
			});
			progress = 100;
			tracker.track({ upload_id: accepted.upload_id, status: accepted.status, rom_id: null, error: null });
		} catch (error) {
			uploadError = getErrorMessage(error, 'common.errors.upload', 'Failed to upload ROM', t);
		} finally {
			uploading = false;
		}
	}

	onMount(() => {
		void i18n.initialize();
		return () => tracker.stop();
	});
</script>

<section class="page-shell stack" aria-labelledby="uploads-title">
	<h1 id="uploads-title">{t('uploads.heading', 'Uploads')}</h1>

	<section class="upload-workspace stack" aria-label={t('uploads.heading', 'Uploads')}>
		<UploadForm {uploading} onselect={resetForNewFile} onupload={(file) => void upload(file)} />
		<UploadProgress percent={progress} />

		{#if tracker.status}
			<UploadStatusCard upload={tracker.status} />
		{:else if !errorMessage}
			<p>{t('uploads.empty', 'No uploads in progress.')}</p>
		{/if}
	</section>

	{#if statusMessage}
		<p class="message" role="status">{statusMessage}</p>
	{/if}

	{#if errorMessage}
		<p class="message message--error" role="alert">{errorMessage}</p>
	{/if}
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

	.message {
		margin: 0;
		padding: 0.75rem 0.85rem;
		border: 2px solid var(--pixel-border);
		background: rgba(18, 22, 31, 0.92);
	}

	.message--error {
		border-color: #e18a78;
		color: #ffd8cf;
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
