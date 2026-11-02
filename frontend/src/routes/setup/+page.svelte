<script lang="ts">
	import { onMount } from 'svelte';
	import ActionButton from '$lib/components/ActionButton.svelte';
	import { completeSetup, getSetupStatus, i18n, testMySQLConnection } from '$lib';
	import type { DatabaseDriver, MySQLConnection, SetupLockedField } from '$lib';
	import { ApiClientError } from '$lib/api/errors';
	import { getErrorMessage } from '$lib/helpers/error';
	import { useTranslate } from '$lib/i18n/helpers';

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

	let testing = $state(false);
	let testResult = $state<{ ok: boolean; message: string } | null>(null);
	let saving = $state(false);
	let errorMessage = $state('');

	const t = useTranslate(i18n);
	const isLocked = (field: SetupLockedField) => locked.includes(field);
	const lockedHint = $derived(t('setup.locked', 'Set by environment variable'));

	function describeError(error: unknown, fallbackKey: string, fallback: string): string {
		if (error instanceof ApiClientError && error.code === 'validation_error' && error.message) {
			return error.message;
		}
		return getErrorMessage(error, fallbackKey, fallback, t);
	}

	function resetTest(): void {
		testResult = null;
	}

	async function runConnectionTest(): Promise<void> {
		testing = true;
		testResult = null;
		try {
			await testMySQLConnection({ ...mysql, port: Number(mysql.port) });
			testResult = { ok: true, message: t('setup.mysql.testSuccess', 'Connection successful.') };
		} catch (error) {
			testResult = { ok: false, message: describeError(error, 'setup.mysql.testFailure', 'Connection failed.') };
		} finally {
			testing = false;
		}
	}

	async function submit(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		errorMessage = '';
		if (adminPassword.length < MIN_PASSWORD_LENGTH) {
			errorMessage = t('setup.admin.passwordTooShort', 'Password must be at least {count} characters.', { count: MIN_PASSWORD_LENGTH });
			return;
		}
		if (adminPassword !== adminPasswordConfirm) {
			errorMessage = t('setup.admin.passwordMismatch', 'Passwords do not match.');
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
			errorMessage = describeError(error, 'setup.saveFailure', 'Could not save the configuration.');
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
				errorMessage = describeError(error, 'setup.loadFailure', 'Could not reach the server.');
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
				<fieldset>
					<legend>{t('setup.database.title', 'Database')}</legend>
					<div class="segmented" role="radiogroup" aria-label={t('setup.database.driver', 'Database driver')}>
						{#each [['sqlite', 'SQLite'], ['mysql', 'MySQL']] as [value, label] (value)}
							<label class:selected={driver === value} class:disabled={isLocked('db_driver')}>
								<input type="radio" name="driver" {value} bind:group={driver} disabled={isLocked('db_driver')} />
								{label}
							</label>
						{/each}
					</div>
					{#if isLocked('db_driver')}<p class="hint">{lockedHint}</p>{/if}

					{#if driver === 'sqlite'}
						<label class="field">
							<span>{t('setup.sqlite.path', 'Database file')}</span>
							<input class="setup-input" bind:value={sqlitePath} required disabled={isLocked('sqlite_path')} />
						</label>
						{#if isLocked('sqlite_path')}<p class="hint">{lockedHint}</p>{/if}
					{:else}
						<div class="field-row">
							<label class="field field--grow">
								<span>{t('setup.mysql.host', 'Host')}</span>
								<input class="setup-input" bind:value={mysql.host} oninput={resetTest} required disabled={isLocked('mysql')} />
							</label>
							<label class="field field--port">
								<span>{t('setup.mysql.port', 'Port')}</span>
								<input class="setup-input" type="number" min="1" max="65535" bind:value={mysql.port} oninput={resetTest} required disabled={isLocked('mysql')} />
							</label>
						</div>
						<label class="field">
							<span>{t('setup.mysql.user', 'User')}</span>
							<input class="setup-input" bind:value={mysql.user} oninput={resetTest} autocomplete="off" required disabled={isLocked('mysql')} />
						</label>
						<label class="field">
							<span>{t('setup.mysql.password', 'Password')}</span>
							<input class="setup-input" type="password" bind:value={mysql.password} oninput={resetTest} autocomplete="new-password" disabled={isLocked('mysql')} />
						</label>
						<label class="field">
							<span>{t('setup.mysql.database', 'Database name')}</span>
							<input class="setup-input" bind:value={mysql.database} oninput={resetTest} required disabled={isLocked('mysql')} />
						</label>
						{#if isLocked('mysql')}<p class="hint">{lockedHint}</p>{/if}
						<div class="test-row">
							<ActionButton class="setup-button setup-button--secondary" type="button" onclick={() => void runConnectionTest()} disabled={testing}>
								{testing ? t('setup.mysql.testing', 'Testing...') : t('setup.mysql.test', 'Test connection')}
							</ActionButton>
							{#if testResult}
								<p class="test-result" class:test-result--ok={testResult.ok} role="status">{testResult.message}</p>
							{/if}
						</div>
					{/if}
				</fieldset>

				<fieldset>
					<legend>{t('setup.storage.title', 'Storage')}</legend>
					<label class="field">
						<span>{t('setup.storage.path', 'ROM storage folder')}</span>
						<input class="setup-input" bind:value={storageRoot} required disabled={isLocked('storage_root')} />
					</label>
					{#if isLocked('storage_root')}<p class="hint">{lockedHint}</p>{/if}
				</fieldset>

				<fieldset>
					<legend>{t('setup.admin.title', 'Administrator')}</legend>
					<label class="field">
						<span>{t('setup.admin.email', 'Email')}</span>
						<input class="setup-input" type="email" bind:value={adminEmail} autocomplete="email" placeholder="admin@example.com" required />
					</label>
					<label class="field">
						<span>{t('setup.admin.password', 'Password')}</span>
						<input class="setup-input" type="password" bind:value={adminPassword} autocomplete="new-password" minlength={MIN_PASSWORD_LENGTH} required />
					</label>
					<label class="field">
						<span>{t('setup.admin.passwordConfirm', 'Confirm password')}</span>
						<input class="setup-input" type="password" bind:value={adminPasswordConfirm} autocomplete="new-password" required />
					</label>
				</fieldset>

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

	fieldset {
		display: grid;
		gap: 0.85rem;
		min-width: 0;
		margin: 0;
		padding: 0;
		border: 0;
	}

	legend {
		margin-bottom: 0.85rem;
		padding: 0;
		font-size: 0.65rem;
		color: var(--pixel-text);
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

	.field-row {
		display: flex;
		gap: 0.75rem;
	}

	.field--grow {
		flex: 1;
	}

	.field--port {
		width: 6rem;
	}

	.setup-input {
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

	.setup-input:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.setup-input:disabled {
		opacity: 0.6;
	}

	.segmented {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.5rem;
	}

	.segmented label {
		display: grid;
		place-items: center;
		min-height: 2.4rem;
		border-radius: 0.4rem;
		background: #303030;
		font-size: 0.55rem;
		cursor: pointer;
	}

	.segmented label.selected {
		background: var(--pixel-panel-strong);
		outline: 2px solid var(--pixel-focus);
	}

	.segmented label.disabled {
		cursor: not-allowed;
		opacity: 0.6;
	}

	.segmented label:focus-within {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.segmented input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}

	.hint {
		margin: -0.35rem 0 0;
		font-size: 0.45rem;
		color: var(--pixel-muted);
	}

	.test-row {
		display: grid;
		gap: 0.6rem;
		justify-items: start;
	}

	.test-result {
		margin: 0;
		font-size: 0.5rem;
		line-height: 1.6;
		color: #ffd8cf;
		word-break: break-word;
	}

	.test-result--ok {
		color: var(--pixel-focus);
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

		.field--port {
			width: 5rem;
		}
	}
</style>
