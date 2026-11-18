<script lang="ts">
	import type { RomDiscEntry } from '#lib/api/types.js';
	import { formatBytes } from '#lib/helpers/format.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import { fileFormat, totalSize } from '#lib/rom/files.js';

	/** Every file of the ROM: one for most ROMs, one per disc for a title of several. */
	let { files, selected }: { files: RomDiscEntry[]; selected?: number } = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
	const multi = $derived(files.length > 1);
</script>

<div class="files">
	<h2 id={`${id}-title`}>{multi ? t('rom.files.titleMany', 'Files') : t('rom.files.title', 'File')}</h2>
	<table aria-labelledby={`${id}-title`}>
		<thead>
			<tr>
				{#if multi}<th scope="col">{t('rom.files.disc', 'Disc')}</th>{/if}
				<th scope="col">{t('rom.files.name', 'Name')}</th>
				<th scope="col">{t('rom.files.format', 'Format')}</th>
				<th scope="col">{t('rom.disc.size', 'Size')}</th>
				<th scope="col">SHA-1</th>
				<th scope="col">CRC32</th>
			</tr>
		</thead>
		<tbody>
			{#each files as file (file.rom_file_id || file.number)}
				<tr aria-current={multi && file.number === selected ? 'true' : undefined}>
					{#if multi}<td data-label={t('rom.files.disc', 'Disc')}>{file.number}</td>{/if}
					<td class="name" data-label={t('rom.files.name', 'Name')}>{file.filename}</td>
					<td data-label={t('rom.files.format', 'Format')}>{fileFormat(file) || '—'}</td>
					<td data-label={t('rom.disc.size', 'Size')}>{formatBytes(file.size, i18n.locale)}</td>
					<td class="mono" data-label="SHA-1" title={`SHA-1 ${file.hash.sha1}\nMD5 ${file.hash.md5}\nSHA-256 ${file.hash.sha256}`}>{file.hash.sha1}</td>
					<td class="mono" data-label="CRC32">{file.hash.crc32 ?? '—'}</td>
				</tr>
			{/each}
		</tbody>
		{#if multi}
			<tfoot>
				<tr>
					<th scope="row" colspan="3">{t('rom.files.total', 'Total')}</th>
					<td data-label={t('rom.files.total', 'Total')}>{formatBytes(totalSize(files), i18n.locale)}</td>
					<td colspan="2"></td>
				</tr>
			</tfoot>
		{/if}
	</table>
</div>

<style>
	/* Matches the disc tracks table below it. */
	.files {
		display: grid;
		gap: 0.6rem;
		margin-top: 1.25rem;
		min-width: 0;
		container-type: inline-size;
	}

	h2 {
		margin: 0;
		font-size: 0.65rem;
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

	/* The disc shown below (tracks, downloads) is marked in the list. */
	tr[aria-current='true'] td {
		background: var(--pixel-panel-strong);
	}

	tfoot td,
	tfoot th {
		border-bottom: 0;
	}

	.name {
		white-space: normal;
		overflow-wrap: anywhere;
	}

	.mono {
		font-family: ui-monospace, monospace;
		font-size: 0.7rem;
		white-space: normal;
		word-break: break-all;
	}

	/* Too narrow for columns: each file becomes a block of labelled rows. */
	@container (max-width: 34rem) {
		thead {
			display: none;
		}

		table,
		tbody,
		tfoot,
		tr {
			display: block;
		}

		tr {
			padding-block: 0.5rem;
			border-bottom: 1px solid var(--pixel-border);
		}

		tfoot th,
		tfoot td:empty {
			display: none;
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
