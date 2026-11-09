<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { createShareLink } from '#lib/api/client.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	/** Makes a single-use share link for a ROM, a new one on each click. */
	let { romId }: { romId: string } = $props();

	const t = useTranslate(i18n);
	let link = $state('');
	let creating = $state(false);
	let copied = $state(false);
	let message = $state('');
	let field = $state<HTMLInputElement>();

	async function create(): Promise<void> {
		creating = true;
		copied = false;
		message = '';
		try {
			const created = await createShareLink(romId);
			link = `${location.origin}${created.url}`;
		} catch (error) {
			message = getErrorMessage(error, 'share.error', 'Could not create a share link.', t);
		} finally {
			creating = false;
		}
	}

	async function copy(): Promise<void> {
		try {
			await navigator.clipboard.writeText(link);
			copied = true;
		} catch {
			// Clipboard access needs HTTPS; selecting the link lets the person copy it themselves.
			field?.select();
		}
	}
</script>

<div class="share">
	<ActionButton type="button" variant="secondary" onclick={create} disabled={creating} aria-busy={creating} title={t('share.hint', 'Anyone signed in can use this link once.')}>
		{creating ? t('share.creating', 'Creating link...') : t('share.button', 'Share')}
	</ActionButton>
	{#if link}
		<div class="share-link">
			<label class="visually-hidden" for="share-link-field">{t('share.linkLabel', 'Share link')}</label>
			<input id="share-link-field" bind:this={field} value={link} readonly onfocus={(event) => event.currentTarget.select()} />
			<ActionButton type="button" variant="secondary" onclick={copy}>
				{copied ? t('share.copied', 'Copied') : t('share.copy', 'Copy')}
			</ActionButton>
		</div>
		<p class="share-hint">{t('share.hint', 'Anyone signed in can use this link once.')}</p>
	{/if}
	{#if message}
		<p class="share-hint" role="alert">{message}</p>
	{/if}
</div>

<style>
	.share {
		display: grid;
		gap: 0.6rem;
		justify-items: start;
		min-width: 0;
	}

	.share-link {
		display: flex;
		gap: 0.5rem;
		width: 100%;
		min-width: 0;
	}

	.share-link input {
		flex: 1;
		min-width: 0;
		font: inherit;
		font-size: 0.55rem;
		padding: 0.45rem;
		background: #202020;
		color: inherit;
		border: 2px solid var(--pixel-panel-strong);
	}

	.share-hint {
		margin: 0;
		color: var(--pixel-muted);
		font-size: 0.55rem;
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
	}
</style>
