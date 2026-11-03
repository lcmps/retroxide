<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import SetupField from '#lib/components/setup/SetupField.svelte';
	import { updateProfile } from '#lib/api/client.js';
	import { ApiClientError } from '#lib/api/errors.js';
	import type { Profile, UIFont, UpdateProfileRequest } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { FONTS, setUserFont } from '#lib/appearance.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		profile: Profile;
		onupdated: (profile: Profile) => void;
	};

	let { profile, onupdated }: Props = $props();

	const t = useTranslate(i18n);
	const MIN_PASSWORD_LENGTH = 8;

	// The form starts from the loaded profile and is reset to it after each save.
	// svelte-ignore state_referenced_locally
	let email = $state(profile.email);
	// svelte-ignore state_referenced_locally
	let username = $state(profile.username ?? '');
	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let saving = $state(false);
	let message = $state('');
	let error = $state('');
	// svelte-ignore state_referenced_locally
	let language = $state(profile.locale ?? '');
	let savingLanguage = $state(false);
	let languageMessage = $state('');
	let languageError = $state('');

	// svelte-ignore state_referenced_locally
	let font = $state<string>(profile.font ?? '');
	let savingFont = $state(false);
	let fontMessage = $state('');
	let fontError = $state('');

	const FONT_LABELS: Record<string, [key: string, fallback: string]> = {
		pixel: ['profile.font.options.pixel', 'Press Start 2P (pixel)'],
		ubuntu: ['profile.font.options.ubuntu', 'Ubuntu'],
		system: ['profile.font.options.system', 'System font']
	};

	async function changeFont(event: Event & { currentTarget: HTMLSelectElement }): Promise<void> {
		const previous = font;
		font = event.currentTarget.value;
		savingFont = true;
		fontMessage = '';
		fontError = '';
		try {
			const updated = await updateProfile({ font: font as UIFont | '' });
			onupdated(updated);
			setUserFont(updated.font);
			fontMessage = t('profile.font.saved', 'Font saved to your account.');
		} catch (caught) {
			font = previous;
			fontError = getValidationAwareErrorMessage(caught, 'profile.font.error', 'Failed to save the font', t);
		} finally {
			savingFont = false;
		}
	}

	/** Language names are shown in their own language, so they are not translated. */
	const LANGUAGE_NAMES: Record<string, string> = { en: 'English', 'pt-BR': 'Português (Brasil)' };

	async function changeLanguage(event: Event & { currentTarget: HTMLSelectElement }): Promise<void> {
		const previous = language;
		language = event.currentTarget.value;
		savingLanguage = true;
		languageMessage = '';
		languageError = '';
		try {
			const updated = await updateProfile({ locale: language });
			onupdated(updated);
			await i18n.applyAccountLocale(updated.locale);
			languageMessage = t('profile.language.saved', 'Language saved to your account.');
		} catch (caught) {
			language = previous;
			languageError = getValidationAwareErrorMessage(caught, 'profile.language.error', 'Failed to save the language', t);
		} finally {
			savingLanguage = false;
		}
	}

	const emailChanged = $derived(email.trim() !== profile.email);
	const usernameChanged = $derived(username.trim().toLowerCase() !== (profile.username ?? ''));
	const passwordChanging = $derived(newPassword.length > 0);
	const passwordProblem = $derived.by(() => {
		if (!passwordChanging) return '';
		if (newPassword.length < MIN_PASSWORD_LENGTH) {
			return t('profile.errors.passwordTooShort', 'The new password must be at least {count} characters.', { count: MIN_PASSWORD_LENGTH });
		}
		if (newPassword !== confirmPassword) return t('profile.errors.passwordMismatch', 'The new passwords do not match.');
		return '';
	});
	const canSave = $derived(
		(emailChanged || usernameChanged || passwordChanging) && currentPassword.length > 0 && !passwordProblem && !saving
	);
	const roleLabel = $derived(
		profile.role === 'admin' ? t('profile.roles.admin', 'Administrator') : t('profile.roles.user', 'User')
	);

	async function save(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (!canSave) return;
		saving = true;
		message = '';
		error = '';
		const request: UpdateProfileRequest = { current_password: currentPassword };
		if (emailChanged) request.email = email.trim();
		if (usernameChanged) request.username = username.trim();
		if (passwordChanging) request.new_password = newPassword;
		try {
			const updated = await updateProfile(request);
			onupdated(updated);
			email = updated.email;
			username = updated.username ?? '';
			message = passwordChanging
				? t('profile.messages.passwordChanged', 'Profile saved. Your password changed, so other devices were signed out.')
				: t('profile.messages.saved', 'Profile saved.');
			currentPassword = newPassword = confirmPassword = '';
		} catch (caught) {
			error =
				caught instanceof ApiClientError && caught.code === 'conflict'
					? t('profile.errors.taken', 'That email or username is already used by another account.')
					: getValidationAwareErrorMessage(caught, 'profile.errors.save', 'Failed to save the profile', t);
		} finally {
			saving = false;
		}
	}
</script>

<section class="card stack" aria-labelledby="profile-card-title">
	<header class="card__header">
		<h2 id="profile-card-title">{t('profile.title', 'Profile')}</h2>
		<span class="role-badge" class:role-badge--admin={profile.role === 'admin'}>{roleLabel}</span>
	</header>

	<div class="language stack">
		<label class="language__label" for="profile-language">{t('profile.language.label', 'Language')}</label>
		<select id="profile-language" class="language__select" value={language} onchange={changeLanguage} disabled={savingLanguage} aria-describedby="profile-language-hint">
			<option value="">{t('profile.language.browser', 'Browser default')}</option>
			{#each i18n.locales as locale (locale)}
				<option value={locale}>{LANGUAGE_NAMES[locale] ?? locale}</option>
			{/each}
		</select>
		<p class="language__hint" id="profile-language-hint">
			{t('profile.language.hint', 'Saved to your account and used on every device, regardless of the browser language.')}
		</p>
		{#if languageError}
			<p class="message message--error" role="alert">{languageError}</p>
		{:else if languageMessage}
			<p class="message" role="status">{languageMessage}</p>
		{/if}
	</div>

	<div class="language stack">
		<label class="language__label" for="profile-font">{t('profile.font.label', 'Font')}</label>
		<select
			id="profile-font"
			class="language__select"
			value={font}
			onchange={changeFont}
			disabled={savingFont}
			aria-describedby="profile-font-hint"
		>
			<option value="">{t('profile.font.default', 'Application default')}</option>
			{#each FONTS as option (option)}
				<option value={option}>{t(...FONT_LABELS[option])}</option>
			{/each}
		</select>
		<p class="language__hint" id="profile-font-hint">
			{t('profile.font.hint', 'Your own font, used everywhere you sign in. The application default is set by an administrator.')}
		</p>
		{#if fontError}
			<p class="message message--error" role="alert">{fontError}</p>
		{:else if fontMessage}
			<p class="message" role="status">{fontMessage}</p>
		{/if}
	</div>

	<form class="stack form" onsubmit={save}>
		<SetupField label={t('profile.fields.email', 'Email')} type="email" autocomplete="email" required bind:value={email} />
		<SetupField
			label={t('profile.fields.username', 'Username')}
			autocomplete="username"
			autocapitalize="none"
			spellcheck="false"
			maxlength={32}
			bind:value={username}
			hint={t('profile.username.hint', 'Optional. Sign in with it instead of your email: 3 to 32 letters, digits, dots, dashes, or underscores.')}
		/>

		<fieldset class="stack">
			<legend>{t('profile.password.title', 'Change password')}</legend>
			<SetupField
				label={t('profile.fields.newPassword', 'New password')}
				type="password"
				autocomplete="new-password"
				bind:value={newPassword}
				hint={t('profile.password.hint', 'Leave empty to keep your current password.')}
			/>
			{#if passwordChanging}
				<SetupField label={t('profile.fields.confirmPassword', 'Confirm new password')} type="password" autocomplete="new-password" bind:value={confirmPassword} />
			{/if}
		</fieldset>

		<SetupField
			label={t('profile.fields.currentPassword', 'Current password')}
			type="password"
			autocomplete="current-password"
			bind:value={currentPassword}
			hint={t('profile.password.currentHint', 'Required to save any change.')}
		/>

		{#if passwordProblem}
			<p class="message message--error" role="alert">{passwordProblem}</p>
		{/if}
		{#if error}
			<p class="message message--error" role="alert">{error}</p>
		{/if}
		{#if message}
			<p class="message" role="status">{message}</p>
		{/if}

		<ActionButton type="submit" disabled={!canSave}>
			{saving ? t('common.status.saving', 'Saving...') : t('profile.actions.save', 'Save profile')}
		</ActionButton>
	</form>

	<p class="card__footnote">{t('profile.fields.userId', 'User ID')}: <span class="mono">{profile.user_id}</span></p>
</section>

<style>
	.card {
		--layout-gap: 1.25rem;
		min-width: 0;
		padding: 1.25rem;
		border: 2px solid var(--pixel-border);
		border-radius: 0.35rem;
		background: var(--pixel-panel);
	}

	.card__header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
	}

	h2 {
		margin: 0;
		font-size: 0.85rem;
	}

	.role-badge {
		padding: 0.35rem 0.6rem;
		border-radius: 0.35rem;
		background: #202020;
		color: var(--pixel-muted);
		font-size: 0.5rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.role-badge--admin {
		background: var(--pixel-accent);
		color: #120d07;
	}

	.form {
		--layout-gap: 1rem;
		padding-top: 1rem;
		border-top: 2px solid #3f3f3f;
	}

	.language {
		--layout-gap: 0.45rem;
	}

	.language__label {
		font-size: 0.5rem;
		color: var(--pixel-muted);
	}

	.language__select {
		width: 100%;
		min-width: 0;
		min-height: 2.4rem;
		padding: 0.5rem 0.75rem;
		border: 0;
		border-radius: 0.5rem;
		background: #303030;
		color: var(--pixel-text);
		font: inherit;
		font-size: 0.6rem;
	}

	.language__select:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.language__hint {
		margin: 0;
		font-size: 0.45rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	fieldset {
		--layout-gap: 0.85rem;
		min-width: 0;
		margin: 0;
		padding: 1rem 0 0;
		border: 0;
		border-top: 2px solid #3f3f3f;
	}

	legend {
		padding: 0 0 0.85rem;
		font-size: 0.6rem;
		color: var(--pixel-text);
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

	.card__footnote {
		margin: 0;
		font-size: 0.45rem;
		color: var(--pixel-muted);
		overflow-wrap: anywhere;
	}

	.mono {
		font-family: ui-monospace, monospace;
		font-size: 0.6rem;
	}
</style>
