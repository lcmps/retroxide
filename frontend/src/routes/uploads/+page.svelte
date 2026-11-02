<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import {
		getUploadProcessingErrorMessage,
		getUploadStatusRetryMessage
	} from '#lib/helpers/error.js';
	import { i18n, uploadRom, uploadStatus } from '#lib';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import type { UploadAcceptedResponse, UploadState, UploadStatusResponse } from '#lib';

	let selectedFile = $state<File | null>(null);
	let uploading = $state(false);
	let uploadProgress = $state(0);
	let errorMessage = $state('');
	let statusMessage = $state('');
	let acceptedUpload = $state<UploadAcceptedResponse | null>(null);
	let latestStatus = $state<UploadStatusResponse | null>(null);

	let pollHandle: ReturnType<typeof setTimeout> | null = null;
	let pollGeneration = 0;
	let pollInFlight = false;
	let pollFailures = 0;
	const MAX_POLL_DELAY_MS = 30_000;

	const t = useTranslate(i18n);

	function getUploadStateLabel(status: UploadState): string {
		if (status === 'completed') {
			return t('uploads.status.completed', 'Completed');
		}

		if (status === 'failed') {
			return t('uploads.status.failed', 'Failed');
		}

		return t('uploads.status.processing', 'Processing upload...');
	}

	function isTerminalStatus(status: UploadState): boolean {
		return status === 'completed' || status === 'failed';
	}

	function clearPolling(): void {
		pollGeneration += 1;
		pollInFlight = false;
		pollFailures = 0;

		if (!pollHandle) {
			return;
		}

		clearTimeout(pollHandle);
		pollHandle = null;
	}

	function schedulePolling(uploadId: string, generation: number, delayMs = 2000): void {
		if (generation !== pollGeneration) {
			return;
		}

		if (pollHandle) {
			clearTimeout(pollHandle);
		}

		pollHandle = setTimeout(() => {
			pollHandle = null;
			void refreshUpload(uploadId, generation);
		}, delayMs);
	}

	function updateSelectedFile(event: Event): void {
		if (!(event.currentTarget instanceof HTMLInputElement)) {
			return;
		}

		selectedFile = event.currentTarget.files?.[0] ?? null;
		uploadProgress = 0;
		errorMessage = '';
		statusMessage = '';
	}

	async function refreshUpload(uploadId: string, generation = pollGeneration): Promise<void> {
		if (generation !== pollGeneration || pollInFlight) {
			return;
		}

		pollInFlight = true;

		try {
			const nextStatus = await uploadStatus(uploadId);
			if (generation !== pollGeneration) {
				return;
			}

			latestStatus = nextStatus;
			pollFailures = 0;
			errorMessage = '';
			statusMessage = t('uploads.messages.accepted', 'Upload accepted. Tracking {uploadId}.', {
				uploadId
			});

			if (isTerminalStatus(nextStatus.status)) {
				clearPolling();
				return;
			}
		} catch (error) {
			if (generation !== pollGeneration) {
				return;
			}

			pollFailures += 1;
			errorMessage = getUploadStatusRetryMessage(t);
			statusMessage = '';
			schedulePolling(uploadId, generation, Math.min(2000 * 2 ** (pollFailures - 1), MAX_POLL_DELAY_MS));
			return;
		} finally {
			pollInFlight = false;
		}

		if (!isTerminalStatus(latestStatus?.status ?? 'processing')) {
			schedulePolling(uploadId, generation);
		}
	}

	function startPolling(uploadId: string): void {
		clearPolling();
		pollFailures = 0;
		schedulePolling(uploadId, pollGeneration, 0);
	}

	async function submitUpload(event: SubmitEvent): Promise<void> {
		event.preventDefault();

		if (!selectedFile) {
			errorMessage = t('uploads.form.none', 'No file selected.');
			return;
		}

		clearPolling();
		uploading = true;
		uploadProgress = 0;
		errorMessage = '';
		statusMessage = '';
		acceptedUpload = null;
		latestStatus = null;

		try {
			const accepted = await uploadRom(selectedFile, (loadedBytes, totalBytes) => {
				uploadProgress = totalBytes > 0 ? Math.round((loadedBytes / totalBytes) * 100) : 0;
			});

			acceptedUpload = accepted;
			latestStatus = {
				upload_id: accepted.upload_id,
				status: accepted.status,
				rom_id: null,
				error: null
			};
			statusMessage = t('uploads.messages.accepted', 'Upload accepted. Tracking {uploadId}.', {
				uploadId: accepted.upload_id
			});
			uploadProgress = 100;

			if (!isTerminalStatus(accepted.status)) {
				startPolling(accepted.upload_id);
			}
		} catch (error) {
			errorMessage = getErrorMessage(error, 'common.errors.upload', 'Failed to upload ROM', t);
		} finally {
			uploading = false;
		}
	}

	onMount(() => {
		void i18n.initialize();

		return () => {
			clearPolling();
		};
	});
</script>

<section class="page-shell stack" aria-labelledby="uploads-title">
	<h1 id="uploads-title">{t('uploads.heading', 'Uploads')}</h1>

	<section class="upload-workspace stack" aria-label={t('uploads.heading', 'Uploads')}>
		<form class="upload-form" onsubmit={submitUpload}>
			<label class="upload-field upload-picker">
				<span>{t('uploads.pick_file', 'Choose a ROM file')}</span>
				<input type="file" accept=".rom,.bin,.nes,.sfc,.smc,.gba,.gb,.gbc,.n64,.z64,.v64,.iso,.cue,.chd" onchange={updateSelectedFile} />
			</label>

			<p class="muted">
				{selectedFile
					? t('uploads.form.selected', 'Selected file: {name}', { name: selectedFile.name })
					: t('uploads.form.none', 'No file selected.')}
			</p>

			<ActionButton type="submit" disabled={uploading || !selectedFile}>
				{uploading
					? t('common.status.uploading', 'Uploading...')
					: t('uploads.form.submit', 'Upload ROM')}
			</ActionButton>
		</form>

		<div class="progress-block" aria-live="polite">
			<label for="upload-progress">{t('uploads.progress.label', 'Upload progress')}</label>
			<progress id="upload-progress" max="100" value={uploadProgress}></progress>
			<span>{uploadProgress}%</span>
		</div>

		{#if !acceptedUpload && !errorMessage}
			<p>{t('uploads.empty', 'No uploads in progress.')}</p>
		{/if}

		{#if acceptedUpload}
			<section class="status-card stack" aria-live="polite">
				<p>{t('uploads.messages.id', 'Upload ID: {uploadId}', { uploadId: acceptedUpload.upload_id })}</p>
				<p>
					{t('uploads.messages.status', 'Status: {status}', {
						status: getUploadStateLabel((latestStatus ?? acceptedUpload).status)
					})}
				</p>

				{#if latestStatus?.rom_id}
					<p>{t('uploads.messages.romId', 'ROM ID: {romId}', { romId: latestStatus.rom_id })}</p>
				{/if}

				{#if latestStatus?.error}
					<p class="message message--error">
						{t('uploads.messages.error', 'Error: {error}', {
							error: getUploadProcessingErrorMessage(latestStatus.error, t)
						})}
					</p>
				{/if}
			</section>
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
	.upload-form,
	.upload-field,
	.progress-block,
	.status-card {
		display: grid;
		gap: 0.75rem;
		min-width: 0;
	}

	h1 { margin: 0; font-size: 1.5rem; }

	.upload-workspace {
		width: min(100%, 38rem);
		padding-top: 1.5rem;
	}

	.upload-field input[type='file'] {
		width: 100%;
		max-width: 100%;
		min-width: 0;
		font-size: 0.55rem;
		overflow: hidden;
	}

	.upload-picker {
		padding: 1.25rem;
		border: 2px dashed var(--pixel-panel-strong);
		color: var(--pixel-muted);
		font-size: 0.65rem;
	}

	.progress-block {
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
	}

	.progress-block label {
		grid-column: 1 / -1;
	}

	.progress-block progress {
		width: 100%;
		height: 1rem;
		accent-color: var(--pixel-accent-strong);
	}

	.status-card {
		padding: 1rem;
		border-left: 0.35rem solid var(--pixel-panel-strong);
		background: #202020;
	}

	.muted {
		margin: 0;
		color: var(--pixel-muted);
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
		h1 { font-size: 1rem; }
		.upload-workspace { padding-top: 0.5rem; }

		.progress-block {
			grid-template-columns: minmax(0, 1fr);
		}

		.progress-block label {
			grid-column: auto;
		}

		.progress-block span {
			justify-self: end;
		}
	}
</style>