<script lang="ts">
	import { browser } from '$app/env';
	import { afterNavigate, goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { Tooltip } from 'bits-ui';
	import AppShell from '#lib/components/AppShell.svelte';
	import { getSetupStatus, hasStoredSession } from '#lib/api/client.js';
	import '@fontsource/press-start-2p';
	import '@fontsource/ubuntu/400.css';
	import '@fontsource/ubuntu/700.css';
	import { applyCachedFont, loadAppearance, loadShownFont } from '#lib/appearance.js';
	import { i18n } from '#lib/i18n/store.svelte.js';
	import '#lib/styles/layout.css';
	import '#lib/styles/pixel.css';

	let { children } = $props();

	const shellPrefixes = ['/library', '/uploads', '/downloads', '/rom', '/profile', '/users'];

	const activePath = $derived(page.url.pathname);
	const useShell = $derived(
		activePath === '/' ||
			shellPrefixes.some((prefix) => activePath === prefix || activePath.startsWith(`${prefix}/`))
	);
	let isAuthenticated = $state(browser && hasStoredSession());
	let setupState = $state<'checking' | 'required' | 'ready'>('checking');

	function enforceRoute(): void {
		if (setupState === 'checking') {
			return;
		}
		if (setupState === 'required') {
			if (activePath !== '/setup') {
				void goto(resolve('/setup'), { replace: true });
			}
			return;
		}
		if (activePath === '/setup') {
			void goto(resolve('/login'), { replace: true });
			return;
		}
		if (useShell && !isAuthenticated) {
			void goto(resolve('/login'), { replace: true });
		}
	}

	/**
	 * iOS Safari zooms in when a field under 16px takes focus, and the page then stays zoomed and
	 * scrolls sideways. A maximum scale stops that while iOS still allows pinching; other browsers
	 * would lose pinch-zoom to it, so only iOS gets it.
	 */
	function preventIosFocusZoom(): void {
		const isIos =
			/iPad|iPhone|iPod/.test(navigator.userAgent) ||
			(/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
		if (isIos) {
			document
				.querySelector('meta[name="viewport"]')
				?.setAttribute('content', 'width=device-width, initial-scale=1, maximum-scale=1');
		}
	}

	/**
	 * The font and the translations decide how wide all text is, so painting before they arrive
	 * reflows the page. A slow connection only delays the first paint this long, not indefinitely.
	 */
	function waitForTextAssets(): Promise<unknown> {
		const timeout = new Promise((done) => setTimeout(done, 1500));
		return Promise.race([Promise.allSettled([loadShownFont(), i18n.initialize()]), timeout]);
	}

	// Start in the last known font to avoid a flash, then follow the server's setting.
	applyCachedFont();
	onMount(() => {
		preventIosFocusZoom();
		void loadAppearance();
		// An unreachable backend is treated as configured so the normal error paths apply.
		const setup = getSetupStatus()
			.then((status): 'required' | 'ready' => (status.required ? 'required' : 'ready'))
			.catch((): 'ready' => 'ready');
		void Promise.all([setup, waitForTextAssets()]).then(([state]) => {
			setupState = state;
			enforceRoute();
		});
	});

	afterNavigate(() => {
		isAuthenticated = hasStoredSession();
		enforceRoute();
	});
</script>

<svelte:head>
	<link rel="icon" type="image/png" href="/components/mini-logo.png" />
	<title>Retroxide</title>
</svelte:head>

<Tooltip.Provider delayDuration={300}>
	{#if setupState === 'checking' || (setupState === 'required' && activePath !== '/setup')}
		<!-- Hold rendering until the setup state is known. -->
	{:else if useShell && isAuthenticated}
		<AppShell activePath={activePath}>
			{@render children()}
		</AppShell>
	{:else if !useShell}
		{@render children()}
	{/if}
</Tooltip.Provider>
