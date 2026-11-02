<script lang="ts">
	import type { Snippet } from 'svelte';

	let { legend, children }: { legend: string; children: Snippet } = $props();
</script>

<fieldset>
	<legend>{legend}</legend>
	{@render children()}
</fieldset>

<style>
	fieldset {
		display: grid;
		gap: 0.85rem;
		min-width: 0;
		margin: 0;
		padding: 0;
		border: 0;
	}

	legend {
		margin-bottom: 0.85rem;
		padding: 0;
		font-size: 0.65rem;
		color: var(--pixel-text);
	}
</style>
