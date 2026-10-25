<script lang="ts">
  import { Dialog } from 'bits-ui';
  import ActionButton from '$lib/components/ActionButton.svelte';
  import { deleteRom, downloadRom, fetchRomCover, getRom, i18n, updateRom, uploadRomCover } from '$lib';
  import { getErrorMessage } from '$lib/helpers/error';
  import { useTranslate } from '$lib/i18n/helpers';
  import type { RomDetail } from '$lib';
  import { onMount } from 'svelte';
  import { onDestroy } from 'svelte';

  let { data } = $props<{ data: { romId: string } }>();

  let rom = $state<RomDetail | null>(null);
  let loading = $state(false);
  let deleting = $state(false);
  let downloading = $state(false);
  let editOpen = $state(false);
  let saving = $state(false);
  let title = $state('');
  let platform = $state('');
  let region = $state('');
  let version = $state('');
  let coverFile = $state<File | null>(null);
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

  async function removeRom() {
    if (!rom) {
      return;
    }

    deleting = true;
    message = '';

    try {
      await deleteRom(rom.rom_id);
      message = t('rom.messages.deleteSuccess', 'ROM deleted successfully.');
      rom = null;
    } catch (error) {
      message = getErrorMessage(error, 'rom.messages.deleteFailure', 'Failed to delete ROM', t);
    } finally {
      deleting = false;
    }
  }

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

  function openEditor(): void {
    if (!rom) return;
    title = rom.title;
    platform = rom.platform;
    region = rom.region ?? '';
    version = rom.version ?? '';
		coverFile = null;
    editOpen = true;
  }

  async function saveRom(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!rom) return;
    saving = true;
    try {
    rom = await updateRom(rom.rom_id, { title, platform, region: region || null, version: version || null });
    if (coverFile) {
      rom = await uploadRomCover(rom.rom_id, coverFile);
      await loadCoverPreview();
    }
      editOpen = false;
    } catch (error) {
      message = getErrorMessage(error, 'rom.messages.loadFailure', 'Failed to update ROM', t);
    } finally {
      saving = false;
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
        <p>{rom.region ?? '-'}</p>
        <p>{rom.version ?? '-'}</p>
        <ActionButton type="button" onclick={downloadCurrentRom} disabled={downloading}>
          {downloading ? t('common.status.downloading', 'Downloading...') : t('downloads.actions.start', 'Download')}
        </ActionButton>
    <Dialog.Root bind:open={editOpen}>
      <ActionButton type="button" variant="secondary" onclick={openEditor}>{t('rom.actions.edit', 'Edit')}</ActionButton>
      <Dialog.Portal>
        <Dialog.Overlay class="dialog-overlay" />
        <Dialog.Content class="dialog-content">
          <Dialog.Title>{t('rom.actions.edit', 'Edit ROM')}</Dialog.Title>
          <form class="edit-form stack" onsubmit={saveRom}>
						<label>{t('rom.fields.title', 'Title')} <input bind:value={title} placeholder={t('rom.placeholders.title', 'ROM title')} required /></label>
						<label>{t('rom.fields.platform', 'Platform')} <input bind:value={platform} placeholder={t('rom.placeholders.platform', 'Platform')} required /></label>
						<label>{t('rom.fields.region', 'Region')} <input bind:value={region} placeholder={t('rom.placeholders.region', 'Region')} /></label>
						<label>{t('rom.fields.version', 'Version')} <input bind:value={version} placeholder={t('rom.placeholders.version', 'Version')} /></label>
						<label>Cover image <input type="file" accept="image/png,image/jpeg,image/webp" onchange={(event) => { coverFile = (event.currentTarget as HTMLInputElement).files?.[0] ?? null; }} /></label>
            <div class="dialog-actions"><Dialog.Close class="dialog-cancel" type="button">Cancel</Dialog.Close><ActionButton type="submit" disabled={saving}>{saving ? 'Saving...' : 'Save'}</ActionButton></div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
        <ActionButton class="delete-action" type="button" variant="danger" onclick={removeRom} disabled={deleting}>
          {deleting ? t('rom.actions.deleting', 'Deleting...') : t('rom.actions.delete', 'Delete ROM')}
        </ActionButton>
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
  .rom-card {
    display: grid;
    grid-template-columns: minmax(10rem, 15rem) minmax(14rem, 22rem);
    justify-content: center;
    align-items: center;
    gap: 2.5rem;
    max-width: 48rem;
    margin-inline: auto;
    padding: 2rem;
    background: var(--pixel-panel);
    border-radius: 0.4rem;
  }

  .rom-cover {
    width: 100%;
    aspect-ratio: 2 / 3;
    background: #202020;
		border-radius: 0.5rem;
		overflow: hidden;
  }
	.rom-cover img { display: block; width: 100%; height: 100%; object-fit: cover; image-rendering: auto; }

  .rom-details { --layout-gap: 1rem; text-align: center; }
  .rom-details h1, .rom-details p { margin: 0; }
  .rom-details h1 { font-size: 1.1rem; line-height: 1.5; }
  .rom-details p { color: var(--pixel-muted); font-size: 0.65rem; }
  :global(.delete-action) { justify-self: center; }
  :global(.dialog-overlay) { position: fixed; inset: 0; z-index: 50; background: rgba(0, 0, 0, 0.8); }
  :global(.dialog-content) { position: fixed; z-index: 51; top: 50%; left: 50%; width: min(calc(100% - 2rem), 30rem); transform: translate(-50%, -50%); padding: 1.5rem; background: var(--pixel-panel); }
  :global(.dialog-content label) { display: grid; gap: 0.5rem; font-size: 0.6rem; }
  :global(.dialog-actions) { display: flex; justify-content: end; gap: 0.75rem; }
  :global(.dialog-cancel) { min-height: 2rem; padding: 0.7rem 1rem; border: 0; background: transparent; color: var(--pixel-text); font-size: 0.55rem; font-family: inherit; }

  .message {
    margin: 0;
    color: var(--pixel-muted);
  }

  @media (max-width: 40rem) {
    .rom-card { grid-template-columns: 1fr; gap: 1.5rem; padding: 1.5rem; }
    .rom-cover { width: min(100%, 14rem); justify-self: center; }
    .rom-details h1 { font-size: 0.85rem; }
  }
</style>
