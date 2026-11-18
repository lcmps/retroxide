<script lang="ts">
	import SignedImage from '#lib/components/SignedImage.svelte';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	/** The title screen and in-game screenshot scraped for a ROM; nothing without either. */
	let { title, titleScreenUrl, screenshotUrl }: { title: string; titleScreenUrl?: string | null; screenshotUrl?: string | null } = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
</script>

{#if titleScreenUrl || screenshotUrl}
	<section class="media" aria-labelledby={`${id}-title`}>
		<h2 id={`${id}-title`}>{t('rom.media.title', 'Screenshots')}</h2>
		<div class="media__grid">
			{#if titleScreenUrl}
				<figure>
					<div class="media__frame"><SignedImage url={titleScreenUrl} alt={t('rom.media.titleScreenAlt', 'Title screen of {title}', { title })} lazy /></div>
					<figcaption>{t('rom.media.titleScreen', 'Title screen')}</figcaption>
				</figure>
			{/if}
			{#if screenshotUrl}
				<figure>
					<div class="media__frame"><SignedImage url={screenshotUrl} alt={t('rom.media.screenshotAlt', 'Screenshot of {title}', { title })} lazy /></div>
					<figcaption>{t('rom.media.screenshot', 'In game')}</figcaption>
				</figure>
			{/if}
		</div>
	</section>
{/if}

<style>
	.media {
		display: grid;
		gap: 0.6rem;
		margin-top: 1.25rem;
		min-width: 0;
	}

	h2 {
		margin: 0;
		font-size: 0.65rem;
	}

	.media__grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr));
		gap: 0.75rem;
	}

	figure {
		display: grid;
		gap: 0.4rem;
		margin: 0;
	}

	/* Retro games are mostly 4:3; pixel art stays sharp when scaled up. */
	.media__frame {
		aspect-ratio: 4 / 3;
		background: #202020;
		overflow: hidden;
	}

	.media__frame :global(img) {
		object-fit: contain;
		image-rendering: pixelated;
	}

	figcaption {
		color: var(--pixel-muted);
		font-size: 0.5rem;
	}
</style>
