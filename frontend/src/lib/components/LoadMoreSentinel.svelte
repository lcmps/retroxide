<script lang="ts">
	type Props = {
		/** Called whenever the sentinel is near the viewport. */
		onvisible: () => void;
		/** Changes to this value re-check visibility, e.g. the number of items loaded so far. */
		key?: unknown;
	};

	let { onvisible, key }: Props = $props();
	let element = $state<HTMLElement>();

	// Re-observing on every key change re-delivers the current intersection, so a page that
	// does not yet fill the viewport keeps loading until it does.
	$effect(() => {
		void key;
		if (!element) {
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					onvisible();
				}
			},
			{ rootMargin: '400px' }
		);
		observer.observe(element);
		return () => observer.disconnect();
	});
</script>

<div class="load-more-sentinel" bind:this={element} aria-hidden="true"></div>

<style>
	.load-more-sentinel {
		height: 1px;
	}
</style>
