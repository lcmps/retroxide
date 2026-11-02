<script lang="ts">
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { Dialog } from 'bits-ui';
  import ActionButton from '$lib/components/ActionButton.svelte';
  import { deleteRom, downloadRom, fetchRomCover, getRom, i18n, scrapeRom } from '$lib';
  import { ApiClientError } from '$lib/api/errors';
  import { getErrorMessage } from '$lib/helpers/error';
  import { useTranslate } from '$lib/i18n/helpers';
  import type { RomDetail } from '$lib';
  import { onMount } from 'svelte';
  import { onDestroy } from 'svelte';

  let { data } = $props<{ data: { romId: string } }>();

  let rom = $state<RomDetail | null>(null);
  let loading = $state(false);
  let downloading = $state(false);
  let scraping = $state(false);
  let deleting = $state(false);
  let deleteOpen = $state(false);
  let coverPreviewUrl = $state<string | null>(null);
  let message = $state('');

  const t = useTranslate(i18n);

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
    coverPreviewUrl = URL.createObjectURL(await fetchRomCover(rom.rom_id));
  }

  onDestroy(() => {
    if (coverPreviewUrl) URL.revokeObjectURL(coverPreviewUrl);
  });

  async function downloadCurrentRom(): Promise<void> {
    if (!rom) {
      return;
    }

    downloading = true;
    message = '';
    try {
      const result = await downloadRom(rom.rom_id);
      const url = URL.createObjectURL(result.blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = result.filename;
      anchor.click();
        requestAnimationFrame(() => URL.revokeObjectURL(url));
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

<section class="page-shell stack" aria-labelledby="rom-title">
  {#if loading}
    <p>{t('rom.messages.loading', 'Loading...')}</p>
  {:else if rom}
    <article class="rom-card">
      <div class="rom-cover" aria-hidden="true">
        {#if coverPreviewUrl}<img src={coverPreviewUrl} alt="" />{/if}
      </div>
      <div class="rom-details stack">
        <h1 id="rom-title">{rom.title}</h1>
        <p>{rom.platform}</p>
        {#if rom.publisher}<p>{rom.publisher}</p>{/if}
        {#if rom.release_year}<p>{rom.release_year}</p>{/if}
        <ActionButton type="button" onclick={downloadCurrentRom} disabled={downloading || deleting}>
          {downloading ? t('common.status.downloading', 'Downloading...') : t('rom.actions.download', 'Download')}
        </ActionButton>
        <ActionButton type="button" variant="secondary" onclick={scrapeCurrentRom} disabled={scraping || deleting}>
          {scraping ? t('rom.actions.scraping', 'Scraping...') : t('rom.actions.scrape', 'Scrape')}
        </ActionButton>
        <Dialog.Root bind:open={deleteOpen}>
          <ActionButton type="button" variant="danger" disabled={deleting || scraping || downloading} onclick={() => { deleteOpen = true; }}>
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
                <ActionButton type="button" variant="danger" disabled={deleting} onclick={deleteCurrentRom}>
                  {deleting ? t('rom.actions.deleting', 'Deleting...') : t('rom.delete.confirm', 'Delete permanently')}
                </ActionButton>
              </div>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      </div>
    </article>
  {:else}
    <h1 id="rom-title">{t('rom.title', 'ROM Detail')}</h1>
    <p>{t('rom.messages.empty', 'No ROM data loaded.')}</p>
  {/if}

  {#if message}
    <p class="message">{message}</p>
  {/if}
</section>

<style>
  :global(.rom-delete-overlay) { position: fixed; inset: 0; z-index: 50; background: rgb(0 0 0 / 75%); }
  :global(.rom-delete-dialog) { position: fixed; inset: 50% auto auto 50%; transform: translate(-50%, -50%); z-index: 51; width: min(32rem, calc(100% - 2rem)); max-height: calc(100dvh - 2rem); overflow-y: auto; padding: 1.5rem; border-radius: 0.4rem; background: var(--pixel-panel); color: var(--pixel-text); }
  :global(.rom-delete-title) { margin: 0; font-size: 0.85rem; line-height: 1.6; }
  :global(.rom-delete-description) { margin: 1rem 0; font-size: 0.6rem; line-height: 1.8; overflow-wrap: anywhere; }
  .rom-delete-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.75rem; }

  .rom-card {
    display: grid;
    justify-items: center;
    gap: 0.5rem;
    max-width: 22rem;
    margin-inline: auto;
    text-align: center;
  }

  .rom-cover {
    width: min(100%, 16rem);
    aspect-ratio: 2 / 3;
    background: #202020;
		border-radius: 0.35rem;
		overflow: hidden;
  }
	.rom-cover img { display: block; width: 100%; height: 100%; object-fit: cover; image-rendering: auto; }

  .rom-details { --layout-gap: 0.5rem; justify-items: center; text-align: center; }
  .rom-details h1, .rom-details p { margin: 0; }
  .rom-details h1 { font-size: 0.9rem; line-height: 1.5; }
  .rom-details p { color: var(--pixel-muted); font-size: 0.65rem; }

  .message {
    margin: 0;
    color: var(--pixel-muted);
  }

  @media (max-width: 40rem) {
    .rom-card { gap: 0.5rem; }
    .rom-cover { width: min(100%, 14rem); }
    .rom-details h1 { font-size: 0.85rem; }
  }
</style>
