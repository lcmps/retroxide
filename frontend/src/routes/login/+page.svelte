<script lang="ts">
	import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { browser } from '$app/environment';
	import { onMount } from 'svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import FormField from '$lib/components/FormField.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
  import { createSession, hasStoredSession, i18n, persistStoredSession } from '$lib';
	import { useTranslate } from '$lib/i18n/helpers';
	import { getErrorMessage } from '$lib/helpers/error';

	let email = $state('');
	let password = $state('');
	let loading = $state(false);
	let resultMessage = $state('');
  const isAuthenticated = $derived(browser && hasStoredSession());

	const t = useTranslate(i18n);

	async function submitLogin(event: SubmitEvent) {
		event.preventDefault();
		loading = true;
		resultMessage = '';

		try {
			const session = await createSession({ email, password });
			persistStoredSession(session);
			resultMessage = t('login.messages.success', 'Logged in successfully. Token type: {tokenType}. Expires in {expiresIn}s.', {
				tokenType: session.token_type,
				expiresIn: session.expires_in
			});
      await goto(resolve('/library'));
		} catch (error) {
			resultMessage = getErrorMessage(error, 'login.messages.failure', 'Login failed', t);
		} finally {
			loading = false;
		}
	}

  onMount(() => {
    if (hasStoredSession()) {
      void goto(resolve('/library'), { replaceState: true });
      return;
    }

    void i18n.initialize().catch((error: unknown) => {
      resultMessage = getErrorMessage(error, 'login.messages.failure', 'Login failed', t);
    });
	});
</script>

{#if !isAuthenticated}
<main class="login-page">
  <section class="login-card stack" aria-labelledby="login-title" style="--layout-gap: 1.75rem">
    <h1 id="login-title" class="visually-hidden">{t('login.cardTitle', 'Session Login')}</h1>
    <img class="login-logo" src="/components/main-logo.png" alt="Retroxide" />

    <form class="login-form stack" onsubmit={submitLogin}>
      <FormField label={t('login.fields.email', 'Email')} class="field visually-hidden-label">
				<span class="field-control">
          <img class="field-icon" src="/components/btns/user.png" alt="" aria-hidden="true" />
					<TextInput
						type="email"
						bind:value={email}
						autocomplete="email"
            aria-label={t('login.fields.email', 'Email')}
            placeholder={t('login.fields.emailPlaceholder', 'user@example.com')}
						required
					/>
				</span>
			</FormField>

      <FormField label={t('login.fields.password', 'Password')} class="field visually-hidden-label">
				<span class="field-control">
          <img class="field-icon" src="/components/btns/lock.png" alt="" aria-hidden="true" />
					<TextInput
						type="password"
						bind:value={password}
						autocomplete="current-password"
            aria-label={t('login.fields.password', 'Password')}
            placeholder={t('login.fields.passwordPlaceholder', 'Password')}
						required
					/>
				</span>
			</FormField>

      <ActionButton class="login-submit" type="submit" disabled={loading}>
        {loading ? t('login.actions.submitting', 'Logging in...') : t('login.actions.submit', 'Login')}
			</ActionButton>
		</form>

    {#if resultMessage}
      <p class="result" role="status">{resultMessage}</p>
    {/if}
  </section>
</main>
{/if}

<style>
  .login-page {
    min-height: 100vh;
    display: grid;
    place-items: center;
    padding: 1.5rem;
    background: var(--pixel-bg);
  }

  .login-card {
    width: min(100%, 18rem);
    justify-items: center;
  }

  .login-logo {
    width: min(100%, 18rem);
    height: auto;
    image-rendering: pixelated;
    image-rendering: crisp-edges;
  }

  .login-form {
    width: 100%;
    --layout-gap: 1rem;
  }

  .visually-hidden,
  :global(.visually-hidden-label .form-field__label) {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  .field-control {
    display: grid;
    grid-template-columns: 3rem minmax(0, 1fr);
    align-items: center;
    gap: 0.9rem;
  }

  .field-icon {
    display: block;
    width: 3rem;
    height: 3rem;
    object-fit: contain;
    image-rendering: pixelated;
  }

  .field-control :global(.text-input) {
    min-height: 2.4rem;
    padding: 0.5rem 0.75rem;
    border: 0;
    border-radius: 0.5rem;
    background: #303030;
    color: var(--pixel-text);
  }

  .field-control :global(.text-input:focus-visible) {
    outline: 2px solid var(--pixel-focus);
    outline-offset: 2px;
  }

  :global(.login-submit) {
    justify-self: center;
    min-width: 5rem;
    min-height: 2.5rem;
    margin-top: 0.5rem;
    padding: 0.5rem 1rem;
    border: 0;
    border-radius: 0.4rem;
    background: var(--pixel-panel-strong);
    color: var(--pixel-text);
    font-family: 'Press Start 2P', monospace;
    font-size: 0.55rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .result {
    width: 100%;
    margin: 0;
    font-size: 0.55rem;
    line-height: 1.6;
    text-align: center;
    color: var(--pixel-muted);
  }

  @media (max-width: 640px) {
    .login-page {
      align-items: start;
      padding: max(3rem, 12vh) 1rem 2rem;
    }

    .login-card {
      width: min(100%, 20rem);
      gap: 1.5rem;
    }

    .field-control {
      grid-template-columns: 2.5rem minmax(0, 1fr);
      gap: 0.65rem;
    }

    .field-icon {
      width: 2.5rem;
      height: 2.5rem;
    }
  }
</style>
