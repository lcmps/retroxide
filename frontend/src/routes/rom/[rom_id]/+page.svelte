<script lang="ts">
  import ActionButton from '$lib/components/ActionButton.svelte';
  import { downloadRom, fetchRomCover, getRom, i18n } from '$lib';
  import { getErrorMessage } from '$lib/helpers/error';
  import { useTranslate } from '$lib/i18n/helpers';
  import type { RomDetail } from '$lib';
  import { onMount } from 'svelte';
  import { onDestroy } from 'svelte';

  let { data } = $props<{ data: { romId: string } }>();

  let rom = $state<RomDetail | null>(null);
  let loading = $state(false);
  let downloading = $state(false);
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
        <ActionButton type="button" onclick={downloadCurrentRom} disabled={downloading}>
          {downloading ? t('common.status.downloading', 'Downloading...') : t('rom.actions.download', 'Download')}
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
