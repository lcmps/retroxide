<script lang="ts">
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		/** Percent compressed; null while waiting its turn, 100 while the result is being checked. */
		percent: number | null;
	};

	let { percent }: Props = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
	const measurable = $derived(percent !== null && percent < 100);
	const label = $derived.by(() => {
		if (percent === null) return t('rom.compression.waiting', 'Waiting to compress...');
		if (percent >= 100) return t('rom.compression.checking', 'Checking the compressed disc...');
		return t('rom.compression.running', 'Compressing to CHD: {percent}%', { percent });
	});
</script>

<div class="compression-progress">
	<span {id}>{label}</span>
	<!-- Without a value the bar shows activity with no amount, for the steps that have none. -->
	{#if measurable}
		<progress aria-labelledby={id} max="100" value={percent}></progress>
	{:else}
		<progress aria-labelledby={id}></progress>
	{/if}
</div>

<style>
	/* Takes a row of its own among the page's action buttons. */
	.compression-progress {
		flex: 1 0 100%;
		display: grid;
		gap: 0.4rem;
		min-width: 0;
		font-size: 0.55rem;
		line-height: 1.6;
	}

	span {
		overflow-wrap: anywhere;
	}

	progress {
		display: block;
		width: 100%;
		height: 1rem;
		accent-color: var(--pixel-accent-strong);
	}
</style>
