<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import { createClient, i18n, isAdminSession, listClients, revokeClient } from '$lib';
	import type { ClientSummary } from '$lib';
	import { useTranslate } from '$lib/i18n/helpers';
	import { getErrorMessage } from '$lib/helpers/error';

	let clients = $state<ClientSummary[]>([]);
	let name = $state('');
	let createdToken = $state('');
	let loading = $state(false);
	let saving = $state(false);
	let copyingToken = $state(false);
	let errorMessage = $state('');
	let statusMessage = $state('');
	const isAdmin = $derived(isAdminSession());
	const t = useTranslate(i18n);

	async function refreshClients() {
		loading = true;
		errorMessage = '';
		try {
			clients = (await listClients()).item;
		} catch (error) {
			errorMessage = getErrorMessage(error, 'clients.loadFailure', 'Could not load API clients', t);
		} finally {
			loading = false;
		}
	}

	async function submitClient(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		errorMessage = '';
		statusMessage = '';
		createdToken = '';
		try {
			const created = await createClient(name.trim());
			name = '';
			createdToken = created.token;
			statusMessage = t('clients.created', 'Client created. Copy its token now; it is shown only once.');
			await refreshClients();
		} catch (error) {
			errorMessage = getErrorMessage(error, 'clients.createFailure', 'Could not create API client', t);
		} finally {
			saving = false;
		}
	}

	async function removeClient(client: ClientSummary) {
		errorMessage = '';
		statusMessage = '';
		try {
			await revokeClient(client.id);
			statusMessage = t('clients.revoked', 'Client revoked.');
			await refreshClients();
		} catch (error) {
			errorMessage = getErrorMessage(error, 'clients.revokeFailure', 'Could not revoke API client', t);
		}
	}

	async function copyToken(): Promise<void> {
		if (!createdToken) return;
		copyingToken = true;
		try {
			await navigator.clipboard.writeText(createdToken);
			statusMessage = t('clients.tokenCopied', 'Token copied to clipboard.');
		} catch {
			statusMessage = t('clients.tokenCopyFailure', 'Copy failed. Select and copy the token manually.');
		} finally {
			copyingToken = false;
		}
	}

	onMount(() => {
		void i18n.initialize().then(() => isAdmin ? refreshClients() : undefined).catch((error: unknown) => {
			errorMessage = getErrorMessage(error, 'clients.loadFailure', 'Could not load API clients', t);
		});
	});
</script>

<section class="page-shell stack clients-page" aria-labelledby="clients-title">
	<h1 id="clients-title">{t('clients.title', 'API Clients')}</h1>
	{#if !isAdmin}
		<p class="message message--error" role="alert">{t('clients.adminOnly', 'Administrator access is required.')}</p>
	{:else}
		<form class="client-create" onsubmit={submitClient}>
			<label for="client-name">{t('clients.nameLabel', 'Client name')}</label>
			<div class="client-create__controls">
				<input id="client-name" bind:value={name} maxlength="255" required placeholder={t('clients.namePlaceholder', 'Desktop sync')} />
				<ActionButton type="submit" disabled={saving || !name.trim()}>{saving ? t('clients.creating', 'Creating...') : t('clients.create', 'Create client')}</ActionButton>
			</div>
		</form>

		{#if createdToken}
			<div class="client-token" role="status">
				<p>{t('clients.tokenOnce', 'New client token (copy now):')}</p>
				<code>{createdToken}</code>
				<ActionButton type="button" variant="secondary" disabled={copyingToken} onclick={() => void copyToken()}>{copyingToken ? t('clients.copying', 'Copying...') : t('clients.copyToken', 'Copy token')}</ActionButton>
			</div>
		{/if}
		{#if statusMessage}<p class="message" role="status">{statusMessage}</p>{/if}
		{#if loading}
			<p class="muted">{t('clients.loading', 'Loading clients...')}</p>
		{:else if clients.length === 0}
			<p class="muted">{t('clients.empty', 'No API clients yet.')}</p>
		{:else}
			<ul class="client-list">
				{#each clients as client (client.id)}
					<li>
						<div><strong>{client.name}</strong><code>{client.id}</code></div>
						<ActionButton type="button" variant="danger" onclick={() => void removeClient(client)}>{t('clients.revoke', 'Revoke')}</ActionButton>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
	{#if errorMessage}<p class="message message--error" role="alert">{errorMessage}</p>{/if}
</section>

<style>
	.clients-page { width: min(100%, 42rem); }
	h1 { margin: 0; font-size: 1.25rem; }
	.client-create { display: grid; gap: .65rem; }
	.client-create label { color: var(--pixel-muted); font-size: .6rem; }
	.client-create__controls { display: flex; flex-wrap: wrap; gap: .75rem; }
	.client-create input { flex: 1 1 14rem; min-width: 0; min-height: 2.5rem; padding: .65rem; border: 2px solid var(--pixel-panel-strong); background: var(--pixel-bg); color: var(--pixel-text); font: inherit; font-size: .6rem; }
	.client-list { display: grid; gap: .75rem; margin: 0; padding: 0; list-style: none; }
	.client-list li { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: .85rem; border: 2px solid var(--pixel-panel-strong); background: var(--pixel-panel); }
	.client-list li > div { display: grid; gap: .5rem; min-width: 0; }
	.client-list code, .client-token code { overflow-wrap: anywhere; color: var(--pixel-muted); font-size: .55rem; }
	.client-token { display: grid; gap: .5rem; margin: 0; padding: .85rem; border: 2px solid var(--pixel-focus); overflow-wrap: anywhere; }
	.client-token p { margin: 0; }
	@media (max-width: 40rem) { h1 { font-size: .9rem; } .client-list li { align-items: flex-start; } }
</style>