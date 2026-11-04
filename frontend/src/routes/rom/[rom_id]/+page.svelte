<script lang="ts">
  import { goto } from '$app/navigation';
  import type { PageProps } from './$types';
  import { resolve } from '$app/paths';
  import { Dialog } from 'bits-ui';
  import ActionButton from '#lib/components/ActionButton.svelte';
  import RomEditForm from '#lib/components/rom/RomEditForm.svelte';
  import { platforms } from '#lib/platforms/catalog.svelte.js';
  import { tick } from 'svelte';
  import { deleteRom, downloadRom, fetchRomCover, getRom, i18n, scrapeRom } from '#lib';
  import DiscTracks from '#lib/components/rom/DiscTracks.svelte';
  import { requestCompression } from '#lib/api/client.js';
  import type { DownloadFormat, DownloadOption } from '#lib/api/types.js';
  import { ApiClientError } from '#lib/api/errors.js';
  import { getErrorMessage } from '#lib/helpers/error.js';
  import { useTranslate } from '#lib/i18n/helpers.js';
  import type { RomDetail } from '#lib';
  import { onMount } from 'svelte';
  import { onDestroy } from 'svelte';

  let { data }: PageProps = $props();

  let rom = $state<RomDetail | null>(null);
  let loading = $state(false);
  let downloading = $state(false);
  let scraping = $state(false);
  let deleting = $state(false);
  let deleteOpen = $state(false);
  let coverPreviewUrl = $state<string | null>(null);
  let editing = $state(false);
  let editButton = $state<HTMLButtonElement>();
  let message = $state('');

  const t = useTranslate(i18n);

  /** Metadata shown in the details box; empty values are omitted. */
  const fields = $derived(
    rom
      ? [
          { key: 'rom.fields.platform', label: 'Platform', value: rom.platform_name || rom.platform },
          { key: 'rom.fields.region', label: 'Region', value: rom.region },
          { key: 'rom.fields.version', label: 'Version', value: rom.version },
          { key: 'rom.fields.publisher', label: 'Publisher', value: rom.publisher },
          { key: 'rom.fields.release_year', label: 'Release year', value: rom.release_year },
          { key: 'rom.fields.genre', label: 'Genre', value: rom.genre }
        ].filter((field): field is { key: string; label: string; value: string } => Boolean(field.value?.trim()))
      : []
  );

  async function loadRom() {
    loading = true;
    message = '';

    try {
    rom = await getRom(data.romId);
    await loadCoverPreview();
    } catch (error) {
      message = getErrorMessage(error, 'rom.messages.loadFailure', 'Failed to load ROM', t);
      rom = null;
    } finally {
      loading = false;
    }
  }

  async function loadCoverPreview(): Promise<void> {
    if (!rom?.cover_art_url) return;
    if (coverPreviewUrl) URL.revokeObjectURL(coverPreviewUrl);
    coverPreviewUrl = URL.createObjectURL(await fetchRomCover(rom.rom_id, rom.cover_art_url));
  }

  onDestroy(() => {
    if (coverPreviewUrl) URL.revokeObjectURL(coverPreviewUrl);
  });

  /** The formats this ROM downloads in, the one it was uploaded in first. */
  const downloads = $derived<DownloadOption[]>(rom?.downloads?.length ? rom.downloads : [{ format: 'file', extension: '' }]);
  let downloadFormat = $state<DownloadFormat>('file');
  let compressing = $state(false);
  let compressionPoll: ReturnType<typeof setTimeout> | undefined;

  // Start on the first format whenever the ROM, and so its formats, change.
  $effect(() => {
    if (!downloads.some((option) => option.format === downloadFormat)) downloadFormat = downloads[0].format;
  });

  function formatLabel(option: DownloadOption): string {
    switch (option.format) {
      case 'chd':
        return t('rom.download.chd', 'CHD (.chd)');
      case 'iso':
        return t('rom.download.iso', 'ISO (.iso)');
      case 'tracks':
        return option.sheet === 'gdi' ? t('rom.download.gdi', 'GDI and tracks (.zip)') : t('rom.download.cue', 'CUE/BIN (.zip)');
      default:
        return t('rom.download.file', 'Original ({extension})', { extension: option.extension });
    }
  }

  /** While a disc is being compressed, refresh until it is done, so the page shows the result. */
  function watchCompression(): void {
    clearTimeout(compressionPoll);
    if (!rom?.disc?.compression_requested) return;
    compressionPoll = setTimeout(async () => {
      if (!rom) return;
      try {
        const updated = await getRom(rom.rom_id);
        const finished = !updated.disc?.compression_requested;
        rom = updated;
        if (finished) {
          message = updated.disc?.format === 'chd'
            ? t('rom.messages.compressed', 'Compressed to CHD.')
            : t('rom.messages.compressFailed', 'The disc could not be compressed; it is kept as it was.');
        }
      } catch {
        // Try again on the next round.
      }
      watchCompression();
    }, 3000);
  }

  $effect(() => {
    if (rom?.disc?.compression_requested) watchCompression();
    return () => clearTimeout(compressionPoll);
  });

  async function compressCurrentRom(): Promise<void> {
    if (!rom) return;
    compressing = true;
    message = '';
    try {
      rom = await requestCompression(rom.rom_id);
      message = t('rom.messages.compressQueued', 'Compressing to CHD in the background.');
    } catch (error) {
      message = getErrorMessage(error, 'rom.errors.compress', 'Failed to start compressing the disc', t);
    } finally {
      compressing = false;
    }
  }

  async function downloadCurrentRom(format?: DownloadFormat): Promise<void> {
    if (!rom) {
      return;
    }

    downloading = true;
    message = '';
    try {
      // The browser streams the file to disk through a signed link.
      await downloadRom(rom.rom_id, format);
    } catch (error) {
      message = getErrorMessage(error, 'common.errors.download', 'Failed to download ROM', t);
    } finally {
      downloading = false;
    }
  }

  async function scrapeCurrentRom(): Promise<void> {
    if (!rom) {
      return;
    }

    scraping = true;
    message = '';
    try {
      rom = await scrapeRom(rom.rom_id);
      await loadCoverPreview();
      message = t('rom.messages.scrapeSuccess', 'Scrape complete.');
    } catch (error) {
      if (error instanceof ApiClientError && error.code === 'not_found') {
        message = t('rom.messages.scrapeNoMatch', 'No match found for this ROM.');
      } else if (error instanceof ApiClientError && error.code === 'upstream_unavailable') {
        message = t('rom.messages.scrapeUnavailable', 'Metadata services are unavailable. Try again later.');
      } else {
        message = getErrorMessage(error, 'rom.messages.scrapeFailure', 'Scrape failed.', t);
      }
    } finally {
      scraping = false;
    }
  }

  async function closeEditor(): Promise<void> {
    editing = false;
    // Return focus to where editing started, for keyboard and screen-reader users.
    await tick();
    editButton?.focus();
  }

  async function saveEdits(updated: RomDetail): Promise<void> {
    rom = updated;
    message = t('rom.edit.saved', 'Changes saved.');
    // The platform may have changed, which changes the platform filter's options.
    void platforms.refresh();
    await closeEditor();
  }

  async function deleteCurrentRom(): Promise<void> {
    if (!rom || deleting) return;
    deleting = true;
    message = '';
    try {
      await deleteRom(rom.rom_id);
      deleteOpen = false;
      await goto(resolve('/library'));
    } catch (error) {
      deleteOpen = false;
      message = getErrorMessage(error, 'rom.messages.deleteFailure', 'Failed to delete ROM', t);
    } finally {
      deleting = false;
    }
  }

  onMount(() => {
    void i18n.initialize().then(loadRom).catch((error: unknown) => {
      message = getErrorMessage(error, 'rom.messages.loadFailure', 'Failed to load ROM', t);
    });
  });
</script>

<svelte:head>
	<title>{rom?.title ?? t('rom.title', 'ROM Detail')} · Retroxide</title>
</svelte:head>

<section class="page-shell stack" aria-labelledby="rom-title">
  {#if loading}
    <p role="status">{t('rom.messages.loading', 'Loading...')}</p>
  {:else if rom}
    <article class="rom-card">
      <div class="rom-actions" role="group" aria-label={t('rom.actions.label', 'ROM actions')}>
        <div class="rom-download">
          {#if downloads.length > 1}
            <label class="rom-download__format">
              <span class="visually-hidden">{t('rom.download.format', 'Download format')}</span>
              <select bind:value={downloadFormat} disabled={downloading || deleting} title={t('rom.download.format', 'Download format')}>
                {#each downloads as option (option.format)}
                  <option value={option.format}>{formatLabel(option)}</option>
                {/each}
              </select>
            </label>
          {/if}
          <ActionButton type="button" onclick={() => downloadCurrentRom(downloadFormat)} disabled={downloading || deleting} aria-busy={downloading} title={t('rom.actions.downloadHint', 'Save this ROM to your device')}>
            {downloading ? t('common.status.downloading', 'Downloading...') : t('rom.actions.download', 'Download')}
          </ActionButton>
        </div>
        {#if rom.disc?.can_compress || rom.disc?.compression_requested}
          <ActionButton type="button" onclick={compressCurrentRom} disabled={compressing || deleting || rom.disc.compression_requested} aria-busy={rom.disc.compression_requested} title={t('rom.actions.compressHint', 'Compress the disc to CHD in the background; its uploaded files can still be downloaded exactly as they are')}>
            {rom.disc.compression_requested ? t('rom.actions.compressing', 'Compressing to CHD...') : t('rom.actions.compress', 'Compress to CHD')}
          </ActionButton>
        {/if}
        <ActionButton type="button" onclick={scrapeCurrentRom} disabled={scraping || deleting} aria-busy={scraping} title={t('rom.actions.scrapeHint', 'Fetch the title, details, and artwork from online game databases')}>
          {scraping ? t('rom.actions.scraping', 'Scraping...') : t('rom.actions.scrape', 'Scrape')}
        </ActionButton>
        <Dialog.Root bind:open={deleteOpen}>
          <ActionButton type="button" variant="danger" disabled={deleting || scraping || downloading} aria-haspopup="dialog" title={t('rom.actions.deleteHint', 'Permanently remove this ROM and its file')} onclick={() => { deleteOpen = true; }}>
            {t('rom.actions.delete', 'Delete ROM')}
          </ActionButton>
          <Dialog.Portal>
            <Dialog.Overlay class="rom-delete-overlay" />
            <Dialog.Content class="rom-delete-dialog" onEscapeKeydown={(event) => { if (deleting) event.preventDefault(); }} onInteractOutside={(event) => { if (deleting) event.preventDefault(); }}>
              <Dialog.Title class="rom-delete-title">{t('rom.delete.title', 'Delete this ROM?')}</Dialog.Title>
              <Dialog.Description class="rom-delete-description">
                {t('rom.delete.description', 'Permanently delete {title}, its stored file, and related records? Shared files still used by other ROMs will be kept. This cannot be undone.', { title: rom.title })}
              </Dialog.Description>
              <div class="rom-delete-actions">
                <ActionButton type="button" variant="secondary" disabled={deleting} onclick={() => { deleteOpen = false; }}>{t('common.actions.cancel', 'Cancel')}</ActionButton>
                <ActionButton type="button" variant="danger" disabled={deleting} aria-busy={deleting} onclick={deleteCurrentRom}>
                  {deleting ? t('rom.actions.deleting', 'Deleting...') : t('rom.delete.confirm', 'Delete permanently')}
                </ActionButton>
              </div>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      </div>
      <div class="rom-cover" aria-hidden={coverPreviewUrl ? undefined : 'true'}>
        {#if coverPreviewUrl}<img src={coverPreviewUrl} alt={t('rom.cover.alt', 'Cover art for {title}', { title: rom.title })} />{/if}
      </div>
      <div class="rom-details" id="rom-details">
        <div class="rom-details__header">
          <h1 id="rom-title">{rom.title}</h1>
          {#if !editing}
            <button
              bind:this={editButton}
              type="button"
              class="rom-edit-button"
              aria-expanded="false"
              aria-controls="rom-details"
              title={t('rom.edit.hintButton', 'Edit the title and details')}
              onclick={() => (editing = true)}
              disabled={deleting}
            >
              {t('rom.edit.button', 'Edit')}
            </button>
          {/if}
        </div>
        {#if editing}
          <RomEditForm {rom} onsaved={(updated) => void saveEdits(updated)} oncancel={() => void closeEditor()} />
        {:else}
          <dl class="rom-fields">
            {#each fields as field (field.key)}
              <div class="rom-field">
                <dt>{t(field.key, field.label)}</dt>
                <dd>{field.value}</dd>
              </div>
            {/each}
          </dl>
          {#if rom.description}<p class="rom-description">{rom.description}</p>{/if}
          {#if rom.disc}<DiscTracks disc={rom.disc} />{/if}
        {/if}
      </div>
    </article>
  {:else}
    <h1 id="rom-title">{t('rom.title', 'ROM Detail')}</h1>
    <p>{t('rom.messages.empty', 'No ROM data loaded.')}</p>
  {/if}

  <!-- Always present so screen readers announce each new message. -->
  <p class="message" role="status" aria-live="polite">{message}</p>
</section>

<style>
  .rom-download {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: stretch;
  }

  .rom-download__format select {
    height: 100%;
    min-height: 2.4rem;
    padding: 0.4rem 0.6rem;
    border: 2px solid var(--pixel-border-strong);
    border-radius: 0.4rem;
    background: var(--pixel-panel);
    color: var(--pixel-text);
    font: inherit;
    font-size: 0.55rem;
  }

  .rom-download__format select:focus-visible {
    outline: 2px solid var(--pixel-focus);
    outline-offset: 2px;
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
  }

  :global(.rom-delete-overlay) { position: fixed; inset: 0; z-index: 50; background: rgb(0 0 0 / 75%); }
  :global(.rom-delete-dialog) { position: fixed; inset: 50% auto auto 50%; transform: translate(-50%, -50%); z-index: 51; width: min(32rem, calc(100% - 2rem)); max-height: calc(100dvh - 2rem); overflow-y: auto; padding: 1.5rem; border-radius: 0.4rem; background: var(--pixel-panel); color: var(--pixel-text); }
  :global(.rom-delete-title) { margin: 0; font-size: 0.85rem; line-height: 1.6; }
  :global(.rom-delete-description) { margin: 1rem 0; font-size: 0.6rem; line-height: 1.8; overflow-wrap: anywhere; }
  .rom-delete-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.75rem; }

  .rom-card {
    display: grid;
    grid-template-columns: minmax(10rem, 16rem) minmax(0, 1fr);
    align-items: start;
    gap: 1.5rem;
    /* Centre the cover, details, and actions as one block; text inside stays left-aligned. The
       explicit width keeps auto margins from shrinking the block to its content. */
    width: 100%;
    max-width: 56rem;
    margin-inline: auto;
  }

  .rom-cover {
    width: 100%;
    aspect-ratio: 2 / 3;
    background: #202020;
    border-radius: 0.35rem;
    overflow: hidden;
  }
  .rom-cover img { display: block; width: 100%; height: 100%; object-fit: cover; image-rendering: auto; }

  .rom-details {
    display: grid;
    gap: 1rem;
    min-width: 0;
    padding: 1.25rem;
    border: 2px solid var(--pixel-border);
    border-radius: 0.35rem;
    background: var(--pixel-panel);
  }
  .rom-details h1 { margin: 0; font-size: 0.9rem; line-height: 1.5; overflow-wrap: anywhere; }

  .rom-details__header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .rom-edit-button {
    flex: none;
    min-height: 2rem;
    padding: 0.45rem 0.8rem;
    border: 2px solid var(--pixel-border-strong);
    border-radius: 0.4rem;
    background: transparent;
    color: var(--pixel-text);
    font: inherit;
    font-size: 0.5rem;
    text-transform: uppercase;
    cursor: pointer;
  }

  .rom-edit-button:hover:not(:disabled) {
    background: var(--pixel-panel-strong);
  }

  .rom-edit-button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .rom-fields { display: grid; gap: 0.6rem; margin: 0; }
  .rom-field {
    display: grid;
    grid-template-columns: minmax(6rem, max-content) minmax(0, 1fr);
    gap: 1rem;
    font-size: 0.65rem;
    line-height: 1.6;
  }
  .rom-field dt { color: var(--pixel-muted); }
  .rom-field dd { margin: 0; overflow-wrap: anywhere; }

  .rom-description { margin: 0; color: var(--pixel-muted); font-size: 0.6rem; line-height: 1.8; }

  /* The actions get their own row at the top, above the cover and details. */
  .rom-actions { grid-column: 1 / -1; display: flex; flex-wrap: wrap; gap: 0.75rem; }

  /* Status messages line up with the centred block above them; the extra specificity beats
     the shell's paragraph width cap. */
  .page-shell p.message {
    width: 100%;
    max-width: 56rem;
    margin: 0 auto;
    color: var(--pixel-muted);
    font-size: 0.6rem;
    line-height: 1.6;
  }

  /* An empty live region stays in the accessibility tree but takes no space. */
  .message:empty {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }

  /* Phones held sideways: size the cover to the screen height and keep the actions beside it. */
  @media (orientation: landscape) and (max-height: 500px) {
    .rom-card { grid-template-columns: auto minmax(0, 1fr); gap: 1rem 1.25rem; }
    .rom-cover { width: min(14rem, calc((100dvh - 9rem) * 2 / 3)); }
  }

  @media (max-width: 40rem) {
    .rom-card { grid-template-columns: minmax(0, 1fr); justify-items: center; gap: 1rem; margin-inline: auto; }
    .rom-cover { width: min(100%, 12rem); }
    .rom-details { width: 100%; max-width: 28rem; padding: 1rem; }
    /* The global phone rule centres page titles; this one belongs to the box's left-aligned text. */
    .rom-card .rom-details h1 { font-size: 0.8rem; text-align: start; }
    .rom-field { grid-template-columns: minmax(0, 1fr); gap: 0.15rem; }
    .rom-actions { width: 100%; max-width: 28rem; justify-content: center; }
    .page-shell p.message { max-width: 28rem; text-align: center; }
  }
</style>
