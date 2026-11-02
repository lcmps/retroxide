<script lang="ts">
	import SetupField from '#lib/components/setup/SetupField.svelte';
	import SetupSection from '#lib/components/setup/SetupSection.svelte';
	import { useTranslate } from '#lib/i18n/helpers.js';
	import { i18n } from '#lib/i18n/store.svelte.js';

	type Props = {
		email: string;
		password: string;
		passwordConfirm: string;
		minPasswordLength: number;
	};

	let { email = $bindable(), password = $bindable(), passwordConfirm = $bindable(), minPasswordLength }: Props = $props();

	const t = useTranslate(i18n);
</script>

<SetupSection legend={t('setup.admin.title', 'Administrator')}>
	<SetupField
		label={t('setup.admin.email', 'Email')}
		type="email"
		bind:value={email}
		autocomplete="email"
		placeholder="admin@example.com"
		required
	/>
	<SetupField
		label={t('setup.admin.password', 'Password')}
		type="password"
		bind:value={password}
		autocomplete="new-password"
		minlength={minPasswordLength}
		required
	/>
	<SetupField
		label={t('setup.admin.passwordConfirm', 'Confirm password')}
		type="password"
		bind:value={passwordConfirm}
		autocomplete="new-password"
		required
	/>
</SetupSection>
