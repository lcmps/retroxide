<script lang="ts">
	import { resolveApiLink } from '#lib/api/client.js';

	/**
	 * An image from a signed API URL. A link that stopped working (its token expired while the
	 * page sat open) hides the image instead of showing a broken picture; a new URL shows again.
	 */
	let { url, alt, lazy = false }: { url: string | null | undefined; alt: string; lazy?: boolean } = $props();

	const src = $derived(url ? resolveApiLink(url) : null);
	let failedSrc = $state<string | null>(null);
	const shown = $derived(src && src !== failedSrc ? src : null);
</script>

{#if shown}<img src={shown} {alt} loading={lazy ? 'lazy' : undefined} decoding="async" onerror={() => (failedSrc = shown)} />{/if}

<style>
	img {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
</style>
