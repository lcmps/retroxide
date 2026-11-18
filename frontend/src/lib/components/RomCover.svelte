<script lang="ts">
	import SignedImage from './SignedImage.svelte';

	type Props = {
		romId: string;
		/** The summary's cover_art_url; nothing is shown when it is absent. */
		coverArtUrl?: string | null;
		/** The summary's cover_thumb_url; when present the small copy is shown instead. */
		coverThumbUrl?: string | null;
	};

	// romId stays in the props for callers' sake; the signed URLs already name the ROM.
	let { coverArtUrl, coverThumbUrl }: Props = $props();
</script>

<!-- Loaded lazily as it nears the viewport, from the browser's cache on repeat visits. -->
<span class="rom-cover-image">
	<SignedImage url={coverThumbUrl || coverArtUrl} alt="" lazy />
</span>

<style>
	.rom-cover-image {
		display: block;
		width: 100%;
		height: 100%;
	}
</style>
