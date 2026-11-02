<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import SetupField from '#lib/components/setup/SetupField.svelte';
	import SetupSection from '#lib/components/setup/SetupSection.svelte';
	import { testMySQLConnection } from '#lib/api/client.js';
	import type { DatabaseDriver, MySQLConnection } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		driver: DatabaseDriver;
		sqlitePath: string;
		mysql: MySQLConnection;
		/** Hint for fields locked by environment variables; undefined fields are editable. */
		lockedHints: { driver?: string; sqlitePath?: string; mysql?: string };
	};

	let { driver = $bindable(), sqlitePath = $bindable(), mysql = $bindable(), lockedHints }: Props = $props();

	const DRIVERS: [DatabaseDriver, string][] = [
		['sqlite', 'SQLite'],
		['mysql', 'MySQL']
	];

	const t = useTranslate(i18n);
	let testing = $state(false);
	let testResult = $state<{ ok: boolean; message: string } | null>(null);

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
			testResult = { ok: false, message: getValidationAwareErrorMessage(error, 'setup.mysql.testFailure', 'Connection failed.', t) };
		} finally {
			testing = false;
		}
	}
</script>

<SetupSection legend={t('setup.database.title', 'Database')}>
	<div class="segmented" role="radiogroup" aria-label={t('setup.database.driver', 'Database driver')}>
		{#each DRIVERS as [value, label] (value)}
			<label class:selected={driver === value} class:disabled={lockedHints.driver}>
				<input type="radio" name="driver" {value} bind:group={driver} disabled={!!lockedHints.driver} />
				{label}
			</label>
		{/each}
	</div>
	{#if lockedHints.driver}<p class="setup-hint">{lockedHints.driver}</p>{/if}

	{#if driver === 'sqlite'}
		<SetupField
			label={t('setup.sqlite.path', 'Database file')}
			bind:value={sqlitePath}
			required
			disabled={!!lockedHints.sqlitePath}
			hint={lockedHints.sqlitePath}
		/>
	{:else}
		{@const mysqlLocked = !!lockedHints.mysql}
		<div class="field-row">
			<div class="field-row__grow">
				<SetupField label={t('setup.mysql.host', 'Host')} bind:value={mysql.host} oninput={resetTest} required disabled={mysqlLocked} />
			</div>
			<div class="field-row__port">
				<SetupField
					label={t('setup.mysql.port', 'Port')}
					type="number"
					min="1"
					max="65535"
					bind:value={mysql.port}
					oninput={resetTest}
					required
					disabled={mysqlLocked}
				/>
			</div>
		</div>
		<SetupField label={t('setup.mysql.user', 'User')} bind:value={mysql.user} oninput={resetTest} autocomplete="off" required disabled={mysqlLocked} />
		<SetupField
			label={t('setup.mysql.password', 'Password')}
			type="password"
			bind:value={mysql.password}
			oninput={resetTest}
			autocomplete="new-password"
			disabled={mysqlLocked}
		/>
		<SetupField label={t('setup.mysql.database', 'Database name')} bind:value={mysql.database} oninput={resetTest} required disabled={mysqlLocked} />
		{#if lockedHints.mysql}<p class="setup-hint">{lockedHints.mysql}</p>{/if}

		<div class="test-row">
			<ActionButton class="setup-button setup-button--secondary" type="button" onclick={() => void runConnectionTest()} disabled={testing}>
				{testing ? t('setup.mysql.testing', 'Testing...') : t('setup.mysql.test', 'Test connection')}
			</ActionButton>
			{#if testResult}
				<p class="test-result" class:test-result--ok={testResult.ok} role="status">{testResult.message}</p>
			{/if}
		</div>
	{/if}
</SetupSection>

<style>
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

	.field-row {
		display: flex;
		gap: 0.75rem;
	}

	.field-row__grow {
		display: grid;
		flex: 1;
		min-width: 0;
	}

	.field-row__port {
		display: grid;
		width: 6rem;
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

	@media (max-width: 640px) {
		.field-row__port {
			width: 5rem;
		}
	}
</style>
