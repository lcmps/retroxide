<script lang="ts">
	import { ArrowDown, ArrowUp, ArrowUpDown, Building2, CalendarDays, CaseSensitive, Tag } from '@lucide/svelte';
	import type { RomSortField } from '#lib/api/client.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import { nextSort, SORT_FIELDS, type RomSort } from '#lib/roms/sort.js';

	type Props = {
		sort: RomSort;
		onchange: (sort: RomSort) => void;
	};

	let { sort, onchange }: Props = $props();

	const t = useTranslate(i18n);

	const icons: Record<RomSortField, typeof CaseSensitive> = {
		title: CaseSensitive,
		release: CalendarDays,
		publisher: Building2,
		genre: Tag
	};
	const fieldNames: Record<RomSortField, () => string> = {
		title: () => t('library.sort.fields.title', 'Title'),
		release: () => t('library.sort.fields.release', 'Release date'),
		publisher: () => t('library.sort.fields.publisher', 'Publisher'),
		genre: () => t('library.sort.fields.genre', 'Genre')
	};

	function label(field: RomSortField): string {
		const name = fieldNames[field]();
		if (sort?.field !== field) return t('library.sort.state.none', 'Sort by {field}', { field: name });
		return sort.order === 'asc'
			? t('library.sort.state.asc', 'Sorted by {field}, ascending. Select to sort descending', { field: name })
			: t('library.sort.state.desc', 'Sorted by {field}, descending. Select to stop sorting', { field: name });
	}

	// Announced after a change, so screen reader users hear the new order without moving focus.
	let announcement = $state('');

	function select(field: RomSortField): void {
		const next = nextSort(sort, field);
		announcement = next
			? next.order === 'asc'
				? t('library.sort.announce.asc', 'Sorted by {field}, ascending.', { field: fieldNames[field]() })
				: t('library.sort.announce.desc', 'Sorted by {field}, descending.', { field: fieldNames[field]() })
			: t('library.sort.announce.none', 'Default order.');
		onchange(next);
	}
</script>

<div class="library-sort" role="group" aria-label={t('library.sort.label', 'Sort the library')}>
	{#each SORT_FIELDS as field (field)}
		{@const Icon = icons[field]}
		{@const active = sort?.field === field}
		<button
			type="button"
			class="library-sort__button"
			class:library-sort__button--active={active}
			aria-label={label(field)}
			title={label(field)}
			onclick={() => select(field)}
		>
			<Icon size={16} strokeWidth={2.5} aria-hidden="true" />
			<span class="library-sort__direction" aria-hidden="true">
				{#if active && sort?.order === 'asc'}
					<ArrowUp size={12} strokeWidth={3} />
				{:else if active}
					<ArrowDown size={12} strokeWidth={3} />
				{:else}
					<ArrowUpDown size={12} strokeWidth={2.5} />
				{/if}
			</span>
		</button>
	{/each}
	<span class="app-shell__assistive" role="status">{announcement}</span>
</div>

<style>
	.library-sort {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: flex-end;
		gap: 0.5rem;
	}

	.library-sort__button {
		display: inline-flex;
		align-items: center;
		gap: 0.2rem;
		min-width: 2.75rem;
		min-height: 2.25rem;
		padding: 0.35rem 0.5rem;
		border: 2px solid var(--pixel-border);
		border-radius: 0.4rem;
		background: transparent;
		color: var(--pixel-muted);
		cursor: pointer;
	}

	.library-sort__button:hover {
		border-color: var(--pixel-border-strong);
		color: var(--pixel-text);
	}

	.library-sort__button:focus-visible {
		outline: 3px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.library-sort__button--active {
		border-color: var(--pixel-border-strong);
		background: var(--pixel-panel-strong);
		color: var(--pixel-text);
	}

	.library-sort__direction {
		display: inline-flex;
		opacity: 0.55;
	}

	.library-sort__button--active .library-sort__direction {
		opacity: 1;
	}

	@media (prefers-reduced-motion: no-preference) {
		.library-sort__button {
			transition: background-color 120ms, border-color 120ms, color 120ms;
		}
	}
</style>
