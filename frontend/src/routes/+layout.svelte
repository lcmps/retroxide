<script lang="ts">
	import { browser } from '$app/environment';
	import { afterNavigate, goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { Tooltip } from 'bits-ui';
	import AppShell from '$lib/components/AppShell.svelte';
	import { hasStoredSession } from '$lib/api/client';
	import '@fontsource/press-start-2p';
	import '$lib/styles/layout.css';
	import '$lib/styles/pixel.css';

	let { children } = $props();

	const shellPrefixes = ['/library', '/uploads', '/downloads', '/rom'];

	const activePath = $derived(page.url.pathname);
	const useShell = $derived(
		activePath === '/' ||
			shellPrefixes.some((prefix) => activePath === prefix || activePath.startsWith(`${prefix}/`))
	);
	let isAuthenticated = $state(browser && hasStoredSession());

	afterNavigate(() => {
		isAuthenticated = hasStoredSession();
		if (useShell && !isAuthenticated) {
			void goto(resolve('/login'), { replaceState: true });
		}
	});
</script>

<svelte:head>
	<link rel="icon" type="image/png" href="/components/mini-logo.png" />
	<title>Retroxide</title>
</svelte:head>

<Tooltip.Provider delayDuration={300}>
	{#if useShell && isAuthenticated}
		<AppShell activePath={activePath}>
			{@render children()}
		</AppShell>
	{:else if !useShell}
		{@render children()}
	{/if}
</Tooltip.Provider>
