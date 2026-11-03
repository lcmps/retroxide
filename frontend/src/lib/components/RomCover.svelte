<script module lang="ts">
	import { fetchRomCover } from '#lib/api/client.js';
	import { createLimiter } from '#lib/helpers/concurrency.js';

	/** Caps concurrent cover requests across every cover on the page. */
	const limitCoverRequests = createLimiter(6);
</script>

<script lang="ts">
	type Props = {
		romId: string;
		/** The summary's cover_art_url; nothing is fetched when it is absent. */
		coverArtUrl?: string | null;
		/** The summary's cover_thumb_url; when present the small copy is shown instead. */
		coverThumbUrl?: string | null;
	};

	let { romId, coverArtUrl, coverThumbUrl }: Props = $props();

	let container = $state<HTMLElement>();
	let src = $state<string>();

	// Covers load only once they approach the viewport, so large catalogs do not request every
	// image up front. Versioned URLs let the browser cache answer repeat visits.
	$effect(() => {
		if (!container || !coverArtUrl) {
			return;
		}
		const thumbnail = Boolean(coverThumbUrl);
		const url = coverThumbUrl || coverArtUrl;
		let cancelled = false;
		let objectUrl: string | undefined;

		const observer = new IntersectionObserver(
			(entries) => {
				if (!entries.some((entry) => entry.isIntersecting)) {
					return;
				}
				observer.disconnect();
				void limitCoverRequests(async () => (cancelled ? null : fetchRomCover(romId, url, thumbnail)))
					.then((blob) => {
						if (blob && !cancelled) {
							objectUrl = URL.createObjectURL(blob);
							src = objectUrl;
						}
					})
					.catch(() => undefined);
			},
			{ rootMargin: '200px' }
		);
		observer.observe(container);

		return () => {
			cancelled = true;
			observer.disconnect();
			if (objectUrl) {
				URL.revokeObjectURL(objectUrl);
			}
			src = undefined;
		};
	});
</script>

<span class="rom-cover-image" bind:this={container}>
	{#if src}<img {src} alt="" />{/if}
</span>

<style>
	.rom-cover-image {
		display: block;
		width: 100%;
		height: 100%;
	}

	img {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
</style>
