<script lang="ts">
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	/** Transfer progress as a percentage from 0 to 100. */
	let { percent }: { percent: number } = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
</script>

<div class="progress-block" aria-live="polite">
	<label for={id}>{t('uploads.progress.label', 'Upload progress')}</label>
	<progress {id} max="100" value={percent}></progress>
	<span>{percent}%</span>
</div>

<style>
	.progress-block {
		font-size: 0.55rem;
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 0.75rem;
		min-width: 0;
	}

	label {
		grid-column: 1 / -1;
	}

	progress {
		width: 100%;
		height: 1rem;
		accent-color: var(--pixel-accent-strong);
	}

	@media (max-width: 640px) {
		.progress-block {
			grid-template-columns: minmax(0, 1fr);
		}

		label {
			grid-column: auto;
		}

		span {
			justify-self: end;
		}
	}
</style>
