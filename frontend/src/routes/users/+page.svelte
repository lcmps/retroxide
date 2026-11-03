<script lang="ts">
	import { onMount, tick } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import UserCard from '#lib/components/users/UserCard.svelte';
	import UserForm from '#lib/components/users/UserForm.svelte';
	import { listUsers } from '#lib/api/client.js';
	import { ApiClientError } from '#lib/api/errors.js';
	import type { DeleteUserResponse, ManagedUser } from '#lib/api/types.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	const t = useTranslate(i18n);

	let users = $state<ManagedUser[]>([]);
	let loading = $state(true);
	let error = $state('');
	let forbidden = $state(false);
	let creating = $state(false);
	let message = $state('');
	let newUserButton = $state<HTMLButtonElement>();

	async function load(): Promise<void> {
		loading = true;
		error = '';
		try {
			users = await listUsers();
		} catch (caught) {
			forbidden = caught instanceof ApiClientError && caught.code === 'forbidden';
			error = forbidden
				? t('users.errors.forbidden', 'Only administrators can manage users.')
				: getErrorMessage(caught, 'users.errors.load', 'Failed to load users', t);
		} finally {
			loading = false;
		}
	}

	async function closeCreate(): Promise<void> {
		creating = false;
		await tick();
		newUserButton?.focus();
	}

	async function created(user: ManagedUser): Promise<void> {
		users = [...users, user].sort((a, b) => a.email.localeCompare(b.email));
		message = t('users.messages.created', '{name} was created.', { name: user.username ?? user.email });
		await closeCreate();
	}

	function updated(user: ManagedUser): void {
		users = users.map((existing) => (existing.id === user.id ? user : existing));
	}

	function deleted(user: ManagedUser, transfer: DeleteUserResponse): void {
		users = users.filter((existing) => existing.id !== user.id);
		message = t('users.messages.deleted', '{name} was deleted. {roms} ROMs moved to your account.', {
			name: user.username ?? user.email,
			roms: transfer.transferred_roms
		});
		// Your own card now holds the transferred ROMs; refresh the totals.
		void load();
	}

	onMount(() => {
		void i18n.initialize();
		void load();
	});
</script>

<svelte:head>
	<title>{t('users.heading', 'Users')} · Retroxide</title>
</svelte:head>

<section class="page-shell" aria-labelledby="users-title">
	<div class="users-layout stack">
		<div class="users-heading">
			<h1 id="users-title">{t('users.heading', 'Users')}</h1>
			{#if !forbidden && !creating}
				<button bind:this={newUserButton} type="button" class="new-user" onclick={() => (creating = true)} disabled={loading}>
					{t('users.actions.new', 'New user')}
				</button>
			{/if}
		</div>

		{#if creating}
			<div class="create-card">
				<UserForm onsaved={(user) => void created(user)} oncancel={() => void closeCreate()} />
			</div>
		{/if}

		<p class="message" role="status" aria-live="polite">{message}</p>

		{#if loading && users.length === 0}
			<p class="page-state" role="status">{t('users.loading', 'Loading users...')}</p>
		{:else if error}
			<p class="page-state page-state--error" role="alert">{error}</p>
		{:else}
			<ul class="user-grid" aria-label={t('users.listLabel', 'Accounts')}>
				{#each users as user (user.id)}
					<UserCard {user} onupdated={updated} ondeleted={deleted} />
				{/each}
			</ul>
		{/if}
	</div>
</section>

<style>
	h1 {
		margin: 0;
		font-size: 1.5rem;
	}

	/* Centred as a block like the other pages; text inside stays left-aligned. */
	.users-layout {
		width: 100%;
		max-width: 80rem;
		margin-inline: auto;
	}

	.users-heading {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}

	.new-user {
		min-height: 2.25rem;
		padding: 0.55rem 1rem;
		border: 0;
		border-radius: 0.4rem;
		background: var(--pixel-accent);
		color: #120d07;
		font: inherit;
		font-size: 0.55rem;
		text-transform: uppercase;
		cursor: pointer;
	}

	.new-user:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.create-card {
		max-width: 36rem;
		padding: 1.25rem;
		border: 2px solid var(--pixel-border);
		border-radius: 0.35rem;
		background: var(--pixel-panel);
	}

	.user-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 22rem), 1fr));
		align-items: start;
		gap: 1.25rem;
		margin: 0;
		padding: 0;
	}

	.page-state {
		margin: 0;
		font-size: 0.6rem;
		color: var(--pixel-muted);
	}

	.page-state--error {
		color: #e18a78;
	}

	.message {
		margin: 0;
		font-size: 0.55rem;
		color: var(--pixel-muted);
	}

	.message:empty {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}

		.users-heading {
			justify-content: center;
		}
	}
</style>
