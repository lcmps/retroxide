<script lang="ts">
	import { resolveApiLink } from '#lib/api/client.js';

	type Props = {
		romId: string;
		/** The summary's cover_art_url; nothing is shown when it is absent. */
		coverArtUrl?: string | null;
		/** The summary's cover_thumb_url; when present the small copy is shown instead. */
		coverThumbUrl?: string | null;
	};

	// romId stays in the props for callers' sake; the signed URLs already name the ROM.
	let { coverArtUrl, coverThumbUrl }: Props = $props();

	// The URL carries a signed token, so the browser loads it like any image: lazily as it nears
	// the viewport, from its HTTP cache on repeat visits, with no bearer-authenticated fetch and
	// no blob kept in page memory. Versioned URLs let the cache answer repeat visits.
	const src = $derived.by(() => {
		const url = coverThumbUrl || coverArtUrl;
		return url ? resolveApiLink(url) : null;
	});
	// A cover whose token expired while the page sat open fails quietly rather than showing a
	// broken-image icon; the next list load brings a fresh URL.
	let failedSrc = $state<string | null>(null);
	const shown = $derived(src && src !== failedSrc ? src : null);
</script>

<span class="rom-cover-image">
	{#if shown}<img src={shown} alt="" loading="lazy" decoding="async" onerror={() => (failedSrc = shown)} />{/if}
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
