<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '#lib/components/ActionButton.svelte';
	import AdminAccountFields from '#lib/components/setup/AdminAccountFields.svelte';
	import DatabaseSettings from '#lib/components/setup/DatabaseSettings.svelte';
	import SetupField from '#lib/components/setup/SetupField.svelte';
	import SetupSection from '#lib/components/setup/SetupSection.svelte';
	import { completeSetup, getSetupStatus } from '#lib/api/client.js';
	import type { DatabaseDriver, MySQLConnection, SetupLockedField } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	const MIN_PASSWORD_LENGTH = 8;

	let loading = $state(true);
	let driver = $state<DatabaseDriver>('sqlite');
	let sqlitePath = $state('./retroxide.sqlite3');
	let mysql = $state<MySQLConnection>({ host: '127.0.0.1', port: 3306, user: 'root', password: '', database: 'retroxide' });
	let storageRoot = $state('./storage');
	let adminEmail = $state('');
	let adminPassword = $state('');
	let adminPasswordConfirm = $state('');
	let locked = $state<SetupLockedField[]>([]);
	let saving = $state(false);
	let errorMessage = $state('');

	const t = useTranslate(i18n);
	const lockedHint = (field: SetupLockedField) =>
		locked.includes(field) ? t('setup.locked', 'Set by environment variable') : undefined;

	function validateAdminPassword(): string {
		if (adminPassword.length < MIN_PASSWORD_LENGTH) {
			return t('setup.admin.passwordTooShort', 'Password must be at least {count} characters.', { count: MIN_PASSWORD_LENGTH });
		}
		if (adminPassword !== adminPasswordConfirm) {
			return t('setup.admin.passwordMismatch', 'Passwords do not match.');
		}
		return '';
	}

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		errorMessage = validateAdminPassword();
		if (errorMessage) {
			return;
		}
		saving = true;
		try {
			await completeSetup({
				db_driver: driver,
				sqlite_path: sqlitePath,
				mysql: { ...mysql, port: Number(mysql.port) },
				storage_root: storageRoot,
				admin_email: adminEmail.trim(),
				admin_password: adminPassword
			});
			// Full reload so the layout re-reads setup status from the now-configured backend.
			window.location.assign('/login');
		} catch (error) {
			errorMessage = getValidationAwareErrorMessage(error, 'setup.saveFailure', 'Could not save the configuration.', t);
			saving = false;
		}
	}

	onMount(() => {
		void i18n.initialize().catch(() => undefined);
		void getSetupStatus()
			.then((status) => {
				if (status.defaults) {
					driver = status.defaults.db_driver;
					sqlitePath = status.defaults.sqlite_path;
					mysql = { ...status.defaults.mysql, password: '' };
					storageRoot = status.defaults.storage_root;
				}
				locked = status.locked ?? [];
			})
			.catch((error: unknown) => {
				errorMessage = getValidationAwareErrorMessage(error, 'setup.loadFailure', 'Could not reach the server.', t);
			})
			.finally(() => {
				loading = false;
			});
	});
</script>

<main class="setup-page">
	<section class="setup-card" aria-labelledby="setup-title">
		<img class="setup-logo" src="/components/main-logo.png" alt="Retroxide" />
		<h1 id="setup-title">{t('setup.title', 'Setup')}</h1>
		<p class="setup-intro">{t('setup.intro', 'Configure the server to finish installing Retroxide.')}</p>

		{#if loading}
			<p class="status">{t('setup.loading', 'Loading...')}</p>
		{:else}
			<form class="setup-form" onsubmit={submit}>
				<DatabaseSettings
					bind:driver
					bind:sqlitePath
					bind:mysql
					lockedHints={{ driver: lockedHint('db_driver'), sqlitePath: lockedHint('sqlite_path'), mysql: lockedHint('mysql') }}
				/>

				<SetupSection legend={t('setup.storage.title', 'Storage')}>
					<SetupField
						label={t('setup.storage.path', 'ROM storage folder')}
						bind:value={storageRoot}
						required
						disabled={!!lockedHint('storage_root')}
						hint={lockedHint('storage_root')}
					/>
				</SetupSection>

				<AdminAccountFields
					bind:email={adminEmail}
					bind:password={adminPassword}
					bind:passwordConfirm={adminPasswordConfirm}
					minPasswordLength={MIN_PASSWORD_LENGTH}
				/>

				<ActionButton class="setup-button" type="submit" disabled={saving}>
					{saving ? t('setup.saving', 'Saving...') : t('setup.save', 'Save and continue')}
				</ActionButton>
			</form>
		{/if}

		{#if errorMessage}
			<p class="status status--error" role="alert">{errorMessage}</p>
		{/if}
	</section>
</main>

<style>
	.setup-page {
		min-height: 100vh;
		display: grid;
		place-items: center;
		padding: 2rem 1.5rem;
		background: var(--pixel-bg);
	}

	.setup-card {
		display: grid;
		justify-items: center;
		gap: 1.25rem;
		width: min(100%, 26rem);
	}

	.setup-logo {
		width: min(100%, 18rem);
		height: auto;
		image-rendering: pixelated;
		image-rendering: crisp-edges;
	}

	h1 {
		margin: 0;
		font-size: 0.9rem;
	}

	.setup-intro,
	.status {
		margin: 0;
		font-size: 0.55rem;
		line-height: 1.6;
		text-align: center;
		color: var(--pixel-muted);
	}

	.status--error {
		color: #ffd8cf;
	}

	.setup-form {
		display: grid;
		gap: 1.5rem;
		width: 100%;
	}

	:global(.setup-button) {
		justify-self: center;
		min-height: 2.5rem;
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

	:global(.setup-button--secondary) {
		justify-self: start;
		background: #303030;
	}

	@media (max-width: 640px) {
		.setup-page {
			align-items: start;
			padding: 2rem 1rem;
		}
	}
</style>
