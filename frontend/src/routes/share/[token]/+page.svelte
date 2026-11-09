<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { fetchShareCover, getSharePreview, redeemShareLink } from '#lib/api/client.js';
	import { ApiClientError } from '#lib/api/errors.js';
	import type { SharePreview } from '#lib/api/types.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { loadShare } from '#lib/share/load.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	const t = useTranslate(i18n);
	const token = $derived(page.params.token ?? '');

	/** loading → ready (offer shown) | used (spent or unknown) | failed. */
	let phase = $state<'loading' | 'ready' | 'used' | 'failed'>('loading');
	let preview = $state<SharePreview | null>(null);
	let cover = $state<string>();
	let adding = $state(false);
	let message = $state('');

	onMount(() => {
		void i18n.initialize();
		let cancelled = false;
		let objectUrl: string | undefined;
		void loadShare(token, { preview: getSharePreview, cover: fetchShareCover }).then((loaded) => {
			if (cancelled) return;
			phase = loaded.phase;
			preview = loaded.preview;
			if (loaded.phase === 'ready' && loaded.cover) {
				objectUrl = URL.createObjectURL(loaded.cover);
				cover = objectUrl;
			}
			if (loaded.phase === 'failed') {
				message = getErrorMessage(loaded.error, 'share.page.error', 'Could not open this link. Please try again.', t);
			}
		});
		return () => {
			cancelled = true;
			if (objectUrl) URL.revokeObjectURL(objectUrl);
		};
	});

	async function add(): Promise<void> {
		adding = true;
		message = '';
		try {
			const added = await redeemShareLink(token);
			await goto(resolve('/rom/[rom_id]', { rom_id: added.rom_id }));
		} catch (error) {
			if (error instanceof ApiClientError && error.status === 404) {
				phase = 'used';
			} else if (error instanceof ApiClientError && error.code === 'already_in_library' && preview) {
				preview = { ...preview, already_in_library: true };
			} else {
				message = getErrorMessage(error, 'share.page.error', 'Could not add this ROM. Please try again.', t);
			}
		} finally {
			adding = false;
		}
	}
</script>

<svelte:head>
	<title>{t('share.page.title', 'Shared ROM')} · Retroxide</title>
</svelte:head>

<section class="page-shell stack share-page" aria-labelledby="share-title">
	<h1 id="share-title">{t('share.page.title', 'Shared ROM')}</h1>

	{#if phase === 'loading'}
		<p role="status">{t('share.page.loading', 'Loading...')}</p>
	{:else if phase === 'used'}
		<p role="status">{t('share.page.used', 'This link has already been used or does not exist.')}</p>
	{:else if phase === 'ready' && preview}
		<article class="share-card">
			<div class="share-cover">
				{#if cover}<img src={cover} alt="" />{/if}
			</div>
			<div class="stack share-details" style="--layout-gap: 0.9rem">
				<h2>{preview.title}</h2>
				<p class="muted">
					{preview.platform_name}{#if preview.disc_count > 1} · {t('share.page.discs', '{count} discs', { count: preview.disc_count })}{/if}
				</p>
				{#if preview.already_in_library}
					<p role="status">{t('share.page.already', 'This ROM is already in your library.')}</p>
					<a href={resolve('/library')}>{t('share.page.openLibrary', 'Open your library')}</a>
				{:else}
					<div class="share-add">
						<ActionButton type="button" onclick={add} disabled={adding} aria-busy={adding}>
							{adding ? t('share.page.adding', 'Adding...') : t('share.page.add', 'Add to my library')}
						</ActionButton>
					</div>
				{/if}
			</div>
		</article>
	{/if}

	{#if message}
		<p class="message" role="alert">{message}</p>
	{/if}
</section>

<style>
	.share-card {
		display: flex;
		flex-wrap: wrap;
		gap: 1.5rem;
		align-items: flex-start;
	}

	.share-cover {
		width: 8rem;
		aspect-ratio: 3 / 4;
		background: #222;
		border: 2px solid var(--pixel-panel-strong);
		overflow: hidden;
	}

	.share-cover img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.share-details {
		min-width: 0;
		flex: 1;
	}

	.share-details h2 {
		margin: 0;
		font-size: 0.9rem;
		line-height: 1.5;
		overflow-wrap: anywhere;
	}

	.share-add {
		justify-self: start;
	}

	.muted {
		margin: 0;
		color: var(--pixel-muted);
		font-size: 0.6rem;
	}

	.share-page p {
		font-size: 0.65rem;
		line-height: 1.6;
	}

	.message {
		color: #ffd8cf;
	}
</style>
