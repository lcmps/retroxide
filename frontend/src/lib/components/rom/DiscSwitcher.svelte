<script lang="ts">
	import type { RomDiscEntry } from '#lib/api/types.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	let { discs, selected, onselect }: { discs: RomDiscEntry[]; selected: number; onselect: (number: number) => void } = $props();

	const t = useTranslate(i18n);
</script>

<div class="disc-switcher" role="group" aria-label={t('rom.discs.label', 'Discs')}>
	{#each discs as entry (entry.number)}
		<button type="button" class="disc-switcher__item" aria-pressed={entry.number === selected} onclick={() => onselect(entry.number)}>
			{t('rom.discs.item', 'Disc {number}', { number: entry.number })}
		</button>
	{/each}
</div>

<style>
	.disc-switcher {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
	}

	.disc-switcher__item {
		min-height: 2rem;
		padding: 0.45rem 0.8rem;
		border: 2px solid var(--pixel-border-strong);
		border-radius: 0.4rem;
		background: transparent;
		color: var(--pixel-text);
		font: inherit;
		font-size: 0.55rem;
		cursor: pointer;
	}

	.disc-switcher__item:hover {
		background: var(--pixel-panel-strong);
	}

	.disc-switcher__item[aria-pressed='true'] {
		background: var(--pixel-panel-strong);
		border-color: var(--pixel-focus);
	}

	.disc-switcher__item:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}
</style>
