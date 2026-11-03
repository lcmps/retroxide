<script lang="ts">
	import { onMount, tick } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import SetupField from '#lib/components/setup/SetupField.svelte';
	import { createUser, updateUser } from '#lib/api/client.js';
	import { ApiClientError } from '#lib/api/errors.js';
	import type { ManagedUser, UpdateUserRequest, UserRole } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		/** The account being edited; absent when creating one. */
		user?: ManagedUser;
		onsaved: (user: ManagedUser) => void;
		oncancel: () => void;
	};

	let { user, onsaved, oncancel }: Props = $props();

	const t = useTranslate(i18n);
	const id = $props.id();
	const MIN_PASSWORD_LENGTH = 8;

	// The form edits a copy of the account as it was when editing started.
	// svelte-ignore state_referenced_locally
	let email = $state(user?.email ?? '');
	// svelte-ignore state_referenced_locally
	let username = $state(user?.username ?? '');
	// svelte-ignore state_referenced_locally
	let role = $state<UserRole>(user?.role ?? 'user');
	let password = $state('');
	let saving = $state(false);
	let error = $state('');
	let form = $state<HTMLFormElement>();

	const creating = $derived(!user);
	const passwordProblem = $derived(
		(creating || password.length > 0) && password.length < MIN_PASSWORD_LENGTH
			? t('users.errors.passwordTooShort', 'Passwords must be at least {count} characters.', { count: MIN_PASSWORD_LENGTH })
			: ''
	);
	const canSave = $derived(email.trim() !== '' && !passwordProblem && !saving);

	onMount(() => {
		void tick().then(() => form?.querySelector<HTMLInputElement>('input')?.focus());
	});

	// Escape anywhere in the form cancels, like closing a dialog.
	$effect(() => {
		const cancelOnEscape = (event: KeyboardEvent) => {
			if (event.key === 'Escape' && !saving) {
				event.preventDefault();
				oncancel();
			}
		};
		form?.addEventListener('keydown', cancelOnEscape);
		return () => form?.removeEventListener('keydown', cancelOnEscape);
	});

	async function save(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (!canSave) return;
		saving = true;
		error = '';
		try {
			if (!user) {
				onsaved(await createUser({ email: email.trim(), username: username.trim() || undefined, password, role }));
				return;
			}
			const changes: UpdateUserRequest = {};
			if (email.trim() !== user.email) changes.email = email.trim();
			if (username.trim().toLowerCase() !== (user.username ?? '')) changes.username = username.trim();
			if (role !== user.role) changes.role = role;
			if (password) changes.password = password;
			onsaved(Object.keys(changes).length > 0 ? await updateUser(user.id, changes) : user);
		} catch (caught) {
			error =
				caught instanceof ApiClientError && caught.code === 'conflict'
					? t('users.errors.taken', 'That email or username is already used by another account.')
					: getValidationAwareErrorMessage(caught, 'users.errors.save', 'Failed to save the user', t);
		} finally {
			saving = false;
		}
	}
</script>

<form bind:this={form} class="user-form stack" onsubmit={save} aria-labelledby={`${id}-title`}>
	<h2 id={`${id}-title`}>
		{creating ? t('users.form.createTitle', 'New user') : t('users.form.editTitle', 'Edit {email}', { email: user?.email ?? '' })}
	</h2>

	<SetupField label={t('users.fields.email', 'Email')} type="email" autocomplete="off" required bind:value={email} />
	<SetupField
		label={t('users.fields.username', 'Username')}
		autocomplete="off"
		autocapitalize="none"
		spellcheck="false"
		maxlength={32}
		bind:value={username}
		hint={t('users.form.usernameHint', 'Optional. Lets the user sign in without their email.')}
	/>

	<label class="field">
		<span>{t('users.fields.role', 'Role')}</span>
		<select bind:value={role} disabled={user?.is_self} aria-describedby={user?.is_self ? `${id}-role-hint` : undefined}>
			<option value="user">{t('profile.roles.user', 'User')}</option>
			<option value="admin">{t('profile.roles.admin', 'Administrator')}</option>
		</select>
	</label>
	{#if user?.is_self}
		<p class="hint" id={`${id}-role-hint`}>{t('users.form.selfRole', 'You cannot change your own role.')}</p>
	{/if}

	<SetupField
		label={creating ? t('users.fields.password', 'Password') : t('users.fields.newPassword', 'New password')}
		type="password"
		autocomplete="new-password"
		required={creating}
		bind:value={password}
		hint={creating
			? t('users.form.passwordHint', 'At least 8 characters. Share it with the user so they can sign in.')
			: t('users.form.resetHint', 'Leave empty to keep it. Setting one signs the user out everywhere.')}
	/>

	{#if password && passwordProblem}
		<p class="message message--error" role="alert">{passwordProblem}</p>
	{/if}
	{#if error}
		<p class="message message--error" role="alert">{error}</p>
	{/if}

	<div class="actions">
		<ActionButton type="submit" disabled={!canSave} aria-busy={saving}>
			{saving
				? t('common.status.saving', 'Saving...')
				: creating
					? t('users.actions.create', 'Create user')
					: t('users.actions.save', 'Save changes')}
		</ActionButton>
		<ActionButton type="button" variant="secondary" class="cancel" disabled={saving} onclick={oncancel}>
			{t('common.actions.cancel', 'Cancel')}
		</ActionButton>
	</div>
</form>

<style>
	.user-form {
		--layout-gap: 0.9rem;
		min-width: 0;
	}

	h2 {
		margin: 0;
		font-size: 0.7rem;
		overflow-wrap: anywhere;
	}

	.field {
		display: grid;
		gap: 0.45rem;
		min-width: 0;
	}

	.field span {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	select {
		width: 100%;
		min-height: 2.4rem;
		padding: 0.5rem 0.75rem;
		border: 0;
		border-radius: 0.5rem;
		background: #303030;
		color: var(--pixel-text);
		font: inherit;
		font-size: 0.6rem;
	}

	select:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.hint {
		margin: -0.35rem 0 0;
		font-size: 0.45rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.message {
		margin: 0;
		font-size: 0.55rem;
		line-height: 1.6;
	}

	.message--error {
		color: #e18a78;
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
	}

	.actions :global(.cancel) {
		box-shadow: inset 0 0 0 2px var(--pixel-border);
	}
</style>
