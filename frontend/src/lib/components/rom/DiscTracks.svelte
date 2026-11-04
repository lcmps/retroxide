<script lang="ts">
	import type { RomDisc } from '#lib/api/types.js';
	import { formatBytes } from '#lib/helpers/format.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	let { disc }: { disc: RomDisc } = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
</script>

<div class="disc">
	<h2 id={`${id}-title`}>{t('rom.disc.title', 'Disc tracks')}</h2>
	<p class="disc__format">
		{disc.format === 'chd'
			? t('rom.disc.storedCHD', 'Stored as CHD. Track hashes are those of the original track files, as disc databases list them.')
			: t('rom.disc.storedTracks', 'Stored as its sheet and track files.')}
	</p>
	<!-- Focusable so keyboard users can scroll a table wider than the screen. -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div class="disc__table" role="region" aria-labelledby={`${id}-title`} tabindex="0">
		<table>
			<thead>
				<tr>
					<th scope="col">{t('rom.disc.track', 'Track')}</th>
					<th scope="col">{t('rom.disc.mode', 'Mode')}</th>
					<th scope="col">{t('rom.disc.size', 'Size')}</th>
					<th scope="col">SHA-1</th>
					<th scope="col">CRC32</th>
				</tr>
			</thead>
			<tbody>
				{#each disc.tracks as track (track.number)}
					<tr>
						<td>{track.number}</td>
						<td>{track.mode}</td>
						<td>{formatBytes(track.size, i18n.locale)}</td>
						<td class="mono" title={`SHA-1 ${track.sha1}\nMD5 ${track.md5}`}>{track.sha1}</td>
						<td class="mono">{track.crc32}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
</div>

<style>
	.disc {
		display: grid;
		gap: 0.6rem;
		margin-top: 1.25rem;
		min-width: 0;
	}

	h2 {
		margin: 0;
		font-size: 0.65rem;
	}

	.disc__format {
		margin: 0;
		font-size: 0.5rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.disc__table {
		overflow-x: auto;
	}

	.disc__table:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.5rem;
	}

	th,
	td {
		padding: 0.4rem 0.5rem;
		text-align: left;
		white-space: nowrap;
		border-bottom: 1px solid var(--pixel-border);
	}

	th {
		color: var(--pixel-muted);
		font-weight: 400;
	}

	.mono {
		font-family: ui-monospace, monospace;
		font-size: 0.7rem;
	}
</style>
