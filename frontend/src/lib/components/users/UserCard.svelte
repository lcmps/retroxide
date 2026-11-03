<script lang="ts">
	import { tick } from 'svelte';
	import { Dialog } from 'bits-ui';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import UserForm from '#lib/components/users/UserForm.svelte';
	import { deleteUser, endUserSessions } from '#lib/api/client.js';
	import type { DeleteUserResponse, ManagedUser } from '#lib/api/types.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { formatBytes } from '#lib/helpers/format.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		user: ManagedUser;
		onupdated: (user: ManagedUser) => void;
		ondeleted: (user: ManagedUser, transfer: DeleteUserResponse) => void;
	};

	let { user, onupdated, ondeleted }: Props = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
	const number = (value: number) => new Intl.NumberFormat(i18n.locale).format(value);

	let editing = $state(false);
	let deleteOpen = $state(false);
	let busy = $state<'signout' | 'delete' | null>(null);
	let message = $state('');
	let error = $state('');
	let editButton = $state<HTMLButtonElement>();

	const roleLabel = $derived(user.role === 'admin' ? t('profile.roles.admin', 'Administrator') : t('profile.roles.user', 'User'));

	async function closeEditor(): Promise<void> {
		editing = false;
		await tick();
		editButton?.focus();
	}

	async function saved(updated: ManagedUser): Promise<void> {
		onupdated(updated);
		message = t('users.messages.saved', 'Changes saved.');
		await closeEditor();
	}

	async function signOut(): Promise<void> {
		busy = 'signout';
		message = error = '';
		try {
			await endUserSessions(user.id);
			onupdated({ ...user, active_sessions: 0 });
			message = t('users.messages.signedOut', 'Signed out on every device.');
		} catch (caught) {
			error = getErrorMessage(caught, 'users.errors.signOut', 'Failed to sign the user out', t);
		} finally {
			busy = null;
		}
	}

	async function remove(): Promise<void> {
		busy = 'delete';
		error = '';
		try {
			const transfer = await deleteUser(user.id);
			deleteOpen = false;
			ondeleted(user, transfer);
		} catch (caught) {
			deleteOpen = false;
			error = getErrorMessage(caught, 'users.errors.delete', 'Failed to delete the user', t);
		} finally {
			busy = null;
		}
	}
</script>

<li class="user-card stack" aria-labelledby={`${id}-name`}>
	{#if editing}
		<UserForm {user} onsaved={(updated) => void saved(updated)} oncancel={() => void closeEditor()} />
	{:else}
		<header class="user-card__header">
			<div class="user-card__identity">
				<h2 id={`${id}-name`}>{user.username ?? user.email}</h2>
				{#if user.username}<p class="muted">{user.email}</p>{/if}
			</div>
			<span class="role-badge" class:role-badge--admin={user.role === 'admin'}>{roleLabel}</span>
		</header>
		{#if user.is_self}<p class="muted self">{t('users.labels.self', 'This is you.')}</p>{/if}

		<dl class="stats">
			<div><dt>{t('users.stats.roms', 'ROMs')}</dt><dd>{number(user.rom_count)}</dd></div>
			<div><dt>{t('users.stats.storage', 'Storage')}</dt><dd>{formatBytes(user.storage_bytes, i18n.locale)}</dd></div>
			<div><dt>{t('users.stats.uploads', 'Uploads')}</dt><dd>{number(user.upload_count)}</dd></div>
			<div><dt>{t('users.stats.sessions', 'Active sessions')}</dt><dd>{number(user.active_sessions)}</dd></div>
		</dl>

		<div class="platforms">
			<h3>{t('users.stats.platforms', 'Platforms')}</h3>
			{#if user.platforms.length > 0}
				<ul aria-label={t('users.stats.platformsOf', 'Platforms of {name}', { name: user.username ?? user.email })}>
					{#each user.platforms as platform (platform.key)}
						<li>{platform.name} <span class="count">{number(platform.rom_count)}</span></li>
					{/each}
				</ul>
			{:else}
				<p class="muted">{t('users.stats.noRoms', 'No ROMs yet.')}</p>
			{/if}
		</div>

		<div class="actions" role="group" aria-label={t('users.actions.label', 'Actions for {name}', { name: user.username ?? user.email })}>
			<button bind:this={editButton} type="button" class="small-button" onclick={() => (editing = true)} disabled={busy !== null}>
				{t('users.actions.edit', 'Edit')}
			</button>
			<button
				type="button"
				class="small-button"
				onclick={signOut}
				disabled={busy !== null || user.active_sessions === 0}
				aria-busy={busy === 'signout'}
				title={t('users.actions.signOutHint', 'End every session of this user')}
			>
				{t('users.actions.signOut', 'Sign out everywhere')}
			</button>
			{#if !user.is_self}
				<Dialog.Root bind:open={deleteOpen}>
					<button type="button" class="small-button small-button--danger" aria-haspopup="dialog" disabled={busy !== null} onclick={() => (deleteOpen = true)}>
						{t('users.actions.delete', 'Delete')}
					</button>
					<Dialog.Portal>
						<Dialog.Overlay class="user-delete-overlay" />
						<Dialog.Content class="user-delete-dialog" onEscapeKeydown={(event) => { if (busy) event.preventDefault(); }} onInteractOutside={(event) => { if (busy) event.preventDefault(); }}>
							<Dialog.Title class="user-delete-title">{t('users.delete.title', 'Delete {name}?', { name: user.username ?? user.email })}</Dialog.Title>
							<Dialog.Description class="user-delete-description">
								{t('users.delete.description', 'The account, its API clients, and its sessions are deleted. Its {roms} ROMs, libraries, and uploads move to your account, so nothing is lost. This cannot be undone.', { roms: number(user.rom_count) })}
							</Dialog.Description>
							<div class="user-delete-actions">
								<ActionButton type="button" variant="secondary" disabled={busy !== null} onclick={() => (deleteOpen = false)}>{t('common.actions.cancel', 'Cancel')}</ActionButton>
								<ActionButton type="button" variant="danger" disabled={busy !== null} aria-busy={busy === 'delete'} onclick={remove}>
									{busy === 'delete' ? t('users.delete.deleting', 'Deleting...') : t('users.delete.confirm', 'Delete user')}
								</ActionButton>
							</div>
						</Dialog.Content>
					</Dialog.Portal>
				</Dialog.Root>
			{/if}
		</div>
	{/if}

	{#if error}
		<p class="message message--error" role="alert">{error}</p>
	{:else if message}
		<p class="message" role="status">{message}</p>
	{/if}
</li>

<style>
	:global(.user-delete-overlay) { position: fixed; inset: 0; z-index: 50; background: rgb(0 0 0 / 75%); }
	:global(.user-delete-dialog) { position: fixed; inset: 50% auto auto 50%; transform: translate(-50%, -50%); z-index: 51; width: min(32rem, calc(100% - 2rem)); max-height: calc(100dvh - 2rem); overflow-y: auto; padding: 1.5rem; border-radius: 0.4rem; background: var(--pixel-panel); color: var(--pixel-text); }
	:global(.user-delete-title) { margin: 0; font-size: 0.85rem; line-height: 1.6; overflow-wrap: anywhere; }
	:global(.user-delete-description) { margin: 1rem 0; font-size: 0.6rem; line-height: 1.8; }
	.user-delete-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.75rem; }

	.user-card {
		--layout-gap: 1rem;
		min-width: 0;
		padding: 1.25rem;
		border: 2px solid var(--pixel-border);
		border-radius: 0.35rem;
		background: var(--pixel-panel);
		list-style: none;
	}

	.user-card__header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.75rem;
	}

	.user-card__identity {
		display: grid;
		gap: 0.35rem;
		min-width: 0;
	}

	h2 {
		margin: 0;
		font-size: 0.7rem;
		line-height: 1.5;
		overflow-wrap: anywhere;
	}

	h3 {
		margin: 0 0 0.5rem;
		font-size: 0.5rem;
		color: var(--pixel-muted);
		font-weight: 400;
	}

	.muted {
		margin: 0;
		font-size: 0.5rem;
		line-height: 1.6;
		color: var(--pixel-muted);
		overflow-wrap: anywhere;
	}

	.self {
		margin-top: -0.5rem;
	}

	.role-badge {
		flex: none;
		padding: 0.35rem 0.6rem;
		border-radius: 0.35rem;
		background: #202020;
		color: var(--pixel-muted);
		font-size: 0.45rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.role-badge--admin {
		background: var(--pixel-accent);
		color: #120d07;
	}

	.stats {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 0.75rem;
		margin: 0;
	}

	.stats div {
		display: grid;
		gap: 0.3rem;
		padding: 0.6rem 0.7rem;
		border-radius: 0.35rem;
		background: #262626;
	}

	.stats dt {
		font-size: 0.45rem;
		color: var(--pixel-muted);
	}

	.stats dd {
		margin: 0;
		font-size: 0.7rem;
	}

	.platforms ul {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.platforms li {
		padding: 0.3rem 0.5rem;
		border-radius: 0.35rem;
		background: #262626;
		font-size: 0.5rem;
	}

	.count {
		color: var(--pixel-muted);
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
	}

	.small-button {
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

	.small-button:hover:not(:disabled) {
		background: var(--pixel-panel-strong);
	}

	.small-button--danger {
		border-color: var(--pixel-danger);
	}

	.small-button--danger:hover:not(:disabled) {
		background: var(--pixel-danger);
	}

	.small-button:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.message {
		margin: 0;
		font-size: 0.55rem;
		line-height: 1.6;
		color: var(--pixel-muted);
	}

	.message--error {
		color: #e18a78;
	}
</style>
