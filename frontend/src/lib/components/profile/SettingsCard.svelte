<script lang="ts">
	import ActionButton from '#lib/components/ActionButton.svelte';
	import { updateSettings } from '#lib/api/client.js';
	import { setDefaultFont } from '#lib/appearance.js';
	import type { AppSetting, SettingsResponse } from '#lib/api/types.js';
	import { getValidationAwareErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		settings: SettingsResponse;
		onupdated: (settings: SettingsResponse) => void;
	};

	let { settings, onupdated }: Props = $props();

	const t = useTranslate(i18n);
	const UI_FONT_KEY = 'RETROXIDE_UI_FONT';
	const GROUP_ORDER = ['appearance', 'uploads', 'sessions', 'workers', 'server', 'storage'];
	const GROUP_LABELS: Record<string, [key: string, fallback: string]> = {
		appearance: ['settings.groups.appearance', 'Appearance'],
		uploads: ['settings.groups.uploads', 'Uploads'],
		sessions: ['settings.groups.sessions', 'Sessions'],
		workers: ['settings.groups.workers', 'Background work'],
		server: ['settings.groups.server', 'Server'],
		storage: ['settings.groups.storage', 'Storage']
	};

	/** Edited values by key; a setting absent here shows the value that will apply next. */
	let drafts = $state<Record<string, string>>({});
	let saving = $state(false);
	let message = $state('');
	let error = $state('');

	const groups = $derived.by(() => {
		const byGroup = new Map<string, AppSetting[]>();
		for (const setting of settings.item) {
			byGroup.set(setting.group, [...(byGroup.get(setting.group) ?? []), setting]);
		}
		return [...byGroup.entries()].sort(
			([a], [b]) => (GROUP_ORDER.indexOf(a) + 1 || 99) - (GROUP_ORDER.indexOf(b) + 1 || 99)
		);
	});
	const changes = $derived(
		Object.fromEntries(
			Object.entries(drafts).filter(([key, value]) => {
				const setting = settings.item.find((item) => item.key === key);
				return setting && value.trim() !== nextValue(setting);
			})
		)
	);
	const hasChanges = $derived(Object.keys(changes).length > 0);

	/** The value the next start will use. */
	function nextValue(setting: AppSetting): string {
		return setting.pending_value ?? setting.value;
	}

	function label(setting: AppSetting): string {
		return t(`settings.items.${setting.key}.label`, setting.label);
	}

	function optionLabel(setting: AppSetting, value: string, fallback: string): string {
		return t(`settings.items.${setting.key}.options.${value}`, fallback);
	}

	function description(setting: AppSetting): string {
		return t(`settings.items.${setting.key}.description`, setting.description);
	}

	async function save(event: SubmitEvent): Promise<void> {
		event.preventDefault();
		if (!hasChanges || saving) return;
		saving = true;
		message = '';
		error = '';
		// Snapshot the edits: once the parent receives the saved settings, `changes` recomputes empty.
		const submitted = { ...changes };
		try {
			const updated = await updateSettings(submitted);
			onupdated(updated);
			const savedLive = Object.keys(submitted).every((key) => updated.item.find((item) => item.key === key)?.live);
			// Live settings already apply; the font is applied to this page right away.
			const font = updated.item.find((item) => item.key === UI_FONT_KEY);
			if (font && UI_FONT_KEY in submitted) setDefaultFont(font.value);
			drafts = {};
			message = savedLive
				? t('settings.messages.savedLive', 'Settings saved and applied.')
				: t('settings.messages.saved', 'Settings saved. They take effect after Retroxide restarts.');
		} catch (caught) {
			error = getValidationAwareErrorMessage(caught, 'settings.errors.save', 'Failed to save the settings', t);
		} finally {
			saving = false;
		}
	}
</script>

<section class="card stack" aria-labelledby="settings-card-title">
	<header class="card__header">
		<h2 id="settings-card-title">{t('settings.title', 'Application settings')}</h2>
		<p class="card__subtitle">
			{settings.can_edit
				? t('settings.subtitle.admin', 'Changes are saved to the configuration file and apply after a restart.')
				: t('settings.subtitle.user', 'Only administrators can change these settings.')}
		</p>
	</header>

	{#if settings.restart_required}
		<p class="banner" role="status">{t('settings.messages.restartRequired', 'Some saved changes are waiting for a restart.')}</p>
	{/if}

	<form class="stack form" onsubmit={save}>
		{#each groups as [group, items] (group)}
			<fieldset class="stack">
				<legend>{t(...(GROUP_LABELS[group] ?? [`settings.groups.${group}`, group]))}</legend>
				{#each items as setting (setting.key)}
					{@const id = `setting-${setting.key}`}
					<div class="setting">
						<div class="setting__heading">
							<label for={id}>{label(setting)}</label>
							<code>{setting.key}</code>
						</div>
						{#if setting.editable && setting.options?.length}
							<select
								{id}
								class="setting__input setting__select"
								value={drafts[setting.key] ?? nextValue(setting)}
								onchange={(event) => (drafts[setting.key] = event.currentTarget.value)}
								aria-describedby={`${id}-description`}
							>
								{#each setting.options as option (option.value)}
									<option value={option.value}>{optionLabel(setting, option.value, option.label)}</option>
								{/each}
							</select>
						{:else if setting.editable}
							<input
								{id}
								class="setting__input"
								value={drafts[setting.key] ?? nextValue(setting)}
								oninput={(event) => (drafts[setting.key] = event.currentTarget.value)}
								spellcheck="false"
								autocomplete="off"
								aria-describedby={`${id}-description`}
							/>
						{:else}
							<output {id} class="setting__value" aria-describedby={`${id}-description`}>
								{setting.options?.length
									? optionLabel(setting, setting.value, setting.options.find((option) => option.value === setting.value)?.label ?? setting.value)
									: setting.value || t('settings.values.empty', 'Not set')}
							</output>
						{/if}
						<p class="setting__description" id={`${id}-description`}>{description(setting)}</p>
						{#if setting.pending_value !== null}
							<p class="setting__note setting__note--pending">
								{t('settings.notes.pending', 'Now {value}; changes to {pending} after a restart.', {
									value: setting.value || '—',
									pending: setting.pending_value || '—'
								})}
							</p>
						{/if}
						{#if settings.can_edit && setting.source === 'environment'}
							<p class="setting__note">{t('settings.notes.environment', 'Set by an environment variable, which overrides the configuration file.')}</p>
						{/if}
					</div>
				{/each}
			</fieldset>
		{/each}

		{#if settings.can_edit}
			<p class="setting__note">{t('settings.notes.clear', 'Clear a field to restore its default.')}</p>
			{#if error}
				<p class="message message--error" role="alert">{error}</p>
			{/if}
			{#if message}
				<p class="message" role="status">{message}</p>
			{/if}
			<ActionButton type="submit" disabled={!hasChanges || saving}>
				{saving ? t('common.status.saving', 'Saving...') : t('settings.actions.save', 'Save settings')}
			</ActionButton>
		{/if}
	</form>
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
		display: grid;
		gap: 0.6rem;
	}

	h2 {
		margin: 0;
		font-size: 0.85rem;
	}

	.card__subtitle {
		margin: 0;
		font-size: 0.5rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.banner {
		margin: 0;
		padding: 0.65rem 0.8rem;
		border-left: 0.3rem solid var(--pixel-accent-strong);
		background: #202020;
		font-size: 0.55rem;
		line-height: 1.6;
	}

	.form {
		--layout-gap: 1.25rem;
	}

	fieldset {
		--layout-gap: 1rem;
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

	.setting {
		display: grid;
		gap: 0.45rem;
		min-width: 0;
	}

	.setting__heading {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: 0.25rem 0.75rem;
	}

	.setting__heading label {
		font-size: 0.55rem;
	}

	code {
		font-family: ui-monospace, monospace;
		font-size: 0.6rem;
		color: var(--pixel-muted);
		overflow-wrap: anywhere;
	}

	.setting__input,
	.setting__value {
		display: block;
		width: 100%;
		min-width: 0;
		min-height: 2.4rem;
		padding: 0.6rem 0.75rem;
		border: 0;
		border-radius: 0.5rem;
		background: #303030;
		color: var(--pixel-text);
		font-family: ui-monospace, monospace;
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}

	.setting__value {
		background: #262626;
		color: var(--pixel-muted);
	}

	/* Choices read better in the interface font than in the monospaced value font. */
	.setting__select {
		font-family: inherit;
		font-size: 0.6rem;
	}

	.setting__input:focus-visible {
		outline: 2px solid var(--pixel-focus);
		outline-offset: 2px;
	}

	.setting__description,
	.setting__note {
		margin: 0;
		font-size: 0.45rem;
		line-height: 1.7;
		color: var(--pixel-muted);
	}

	.setting__note--pending {
		color: var(--pixel-accent-strong);
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
