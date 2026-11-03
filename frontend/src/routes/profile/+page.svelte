<script lang="ts">
	import { onMount } from 'svelte';
	import ProfileCard from '#lib/components/profile/ProfileCard.svelte';
	import SettingsCard from '#lib/components/profile/SettingsCard.svelte';
	import { getProfile, getSettings } from '#lib/api/client.js';
	import type { Profile, SettingsResponse } from '#lib/api/types.js';
	import { getErrorMessage } from '#lib/helpers/error.js';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	const t = useTranslate(i18n);

	let profile = $state<Profile | null>(null);
	let settings = $state<SettingsResponse | null>(null);
	let loading = $state(true);
	let error = $state('');

	onMount(() => {
		void i18n.initialize();
		void Promise.all([getProfile(), getSettings()])
			.then(([loadedProfile, loadedSettings]) => {
				profile = loadedProfile;
				settings = loadedSettings;
			})
			.catch((caught: unknown) => {
				error = getErrorMessage(caught, 'profile.errors.load', 'Failed to load your profile', t);
			})
			.finally(() => {
				loading = false;
			});
	});
</script>

<svelte:head>
	<title>{t('profile.heading', 'Profile & settings')} · Retroxide</title>
</svelte:head>

<section class="page-shell" aria-labelledby="profile-title">
	<div class="profile-layout stack">
		<h1 id="profile-title">{t('profile.heading', 'Profile & settings')}</h1>

		{#if loading}
			<p class="page-state">{t('profile.loading', 'Loading...')}</p>
		{:else if error}
			<p class="page-state page-state--error" role="alert">{error}</p>
		{:else if profile && settings}
			<div class="profile-grid">
				<ProfileCard {profile} onupdated={(updated) => (profile = updated)} />
				<SettingsCard {settings} onupdated={(updated) => (settings = updated)} />
			</div>
		{/if}
	</div>
</section>

<style>
	h1 {
		margin: 0;
		font-size: 1.5rem;
	}

	/*
	 * The page's block is centred while its text stays left-aligned, so the title lines up with
	 * the cards' left edge at every width.
	 */
	.profile-layout {
		width: 100%;
		max-width: 80rem;
		margin-inline: auto;
	}

	/* Side by side on wide screens; stacked once the cards would get cramped. */
	.profile-grid {
		display: grid;
		grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
		align-items: start;
		gap: 1.5rem;
	}

	.page-state {
		margin: 0;
		font-size: 0.6rem;
		color: var(--pixel-muted);
	}

	.page-state--error {
		color: #e18a78;
	}

	@media (max-width: 60rem) {
		.profile-layout {
			max-width: 40rem;
		}

		.profile-grid {
			grid-template-columns: minmax(0, 1fr);
		}
	}

	@media (max-width: 640px) {
		h1 {
			font-size: 1rem;
		}

		.page-state {
			text-align: center;
		}
	}
</style>
