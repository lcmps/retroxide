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
	<table aria-labelledby={`${id}-title`}>
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
					<td data-label={t('rom.disc.track', 'Track')}>{track.number}</td>
					<td data-label={t('rom.disc.mode', 'Mode')}>{track.mode}</td>
					<td data-label={t('rom.disc.size', 'Size')}>{formatBytes(track.size, i18n.locale)}</td>
					<td class="mono" data-label="SHA-1" title={`SHA-1 ${track.sha1}\nMD5 ${track.md5}`}>{track.sha1}</td>
					<td class="mono" data-label="CRC32">{track.crc32}</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
	.disc {
		display: grid;
		gap: 0.6rem;
		margin-top: 1.25rem;
		min-width: 0;
		/* The table's room depends on the box around it, not on the width of the screen. */
		container-type: inline-size;
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
		/* Hashes are the only long values; they wrap so the table always fits its box. */
		white-space: normal;
		word-break: break-all;
	}

	/* Too narrow for columns: each track becomes a block of labelled rows. */
	@container (max-width: 34rem) {
		thead {
			display: none;
		}

		table,
		tbody,
		tr {
			display: block;
		}

		tr {
			padding-block: 0.5rem;
			border-bottom: 1px solid var(--pixel-border);
		}

		td {
			display: grid;
			grid-template-columns: 4rem minmax(0, 1fr);
			gap: 0.5rem;
			align-items: baseline;
			padding: 0.25rem 0;
			border: 0;
			white-space: normal;
		}

		td::before {
			content: attr(data-label);
			color: var(--pixel-muted);
			font-family: var(--app-font);
			font-size: 0.5rem;
		}
	}
</style>
