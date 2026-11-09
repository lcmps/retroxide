<script lang="ts">
	import { goto } from "$app/navigation";
	import { resolve } from "$app/paths";
	import { page } from "$app/state";
	import { Gamepad2, Search } from "@lucide/svelte";
	import { DropdownMenu } from "bits-ui";
	import { onMount } from "svelte";
	import type { Snippet } from "svelte";
	import { getProfile, i18n, logger, logoutSession } from "#lib";
	import { uploadQueue } from "#lib/uploads/shared.js";
	import { loginPathFor } from "#lib/share/paths.js";
	import { useTranslate } from "#lib/i18n/helpers.js";
	import { setUserFont } from "#lib/appearance.js";
	import { platforms } from "#lib/platforms/catalog.svelte.js";

	type Props = {
		activePath: string;
		children: Snippet;
	};

	let { activePath, children }: Props = $props();

	let selectedPlatform = $state("");
	// Administrators get the Users page; learned from the profile once signed in.
	let isAdmin = $state(false);
	let searchText = $state("");

	const libraryControlsEnabled = $derived(activePath === "/library");
	// The platforms the library actually holds. A platform named in the URL but not (or no
	// longer) listed still gets an option, so the selection stays visible.
	const platformOptions = $derived.by(() => {
		const options = platforms.items.map((platform) => ({ value: platform.key, label: platform.name }));
		if (selectedPlatform && !options.some((option) => option.value.toLowerCase() === selectedPlatform.toLowerCase())) {
			options.push({ value: selectedPlatform, label: platforms.nameFor(selectedPlatform) });
		}
		return options;
	});
	const translate = useTranslate(i18n);
	const navLinks = $derived([
		{ path: "/library" as const, label: translate("nav.library", "Library") },
		{ path: "/downloads" as const, label: translate("nav.downloads", "Downloads") },
		{ path: "/uploads" as const, label: translate("nav.uploads", "Uploads") },
	]);
	const uploadsInProgress = $derived(
		translate("nav.uploadsInProgress", "Uploads: {count} in progress", { count: uploadQueue.transferringCount }),
	);
	async function updateLibrarySearchParams(
		nextPlatform: string,
		nextQuery: string,
	): Promise<void> {
		if (activePath !== "/library") {
			return;
		}

		const params = new URLSearchParams(page.url.searchParams.toString());
		const trimmedQuery = nextQuery.trim();

		if (nextPlatform) {
			params.set("platform", nextPlatform);
		} else {
			params.delete("platform");
		}

		if (trimmedQuery) {
			params.set("q", trimmedQuery);
		} else {
			params.delete("q");
		}

		const nextUrl =
			params.size > 0 ? `/library?${params.toString()}` : "/library";
		await goto(nextUrl, {
			replace: true,
			reset: false,
		});
	}

	function changePlatform(event: Event): void {
		selectedPlatform = (event.currentTarget as HTMLSelectElement).value;
		void updateLibrarySearchParams(selectedPlatform, searchText);
	}

	function changeSearch(event: Event): void {
		searchText = (event.currentTarget as HTMLInputElement).value;
		void updateLibrarySearchParams(selectedPlatform, searchText);
	}

	/** Whether the page belongs to a link's section: a ROM's page belongs to the library. */
	function inSection(path: string): boolean {
		return isActive(path) || (path === "/library" && activePath.startsWith("/rom/"));
	}

	function isActive(path: string): boolean {
		return activePath === path || activePath.startsWith(`${path}/`);
	}

	async function logout(): Promise<void> {
		// Uploads belong to this account: none may carry on, or finish under the next one signed in.
		uploadQueue.cancel();
		try {
			await logoutSession();
		} catch (error) {
			logger.warn("Server logout failed; clearing the local session anyway.", error);
		}
		// The sign-in page shows the application default font, not this user's.
		setUserFont(null);
		await goto(resolve("/login"), { replace: true });
	}

	// Uploads, scrapes, and deletions change which platforms exist; refresh on every navigation.
	$effect(() => {
		void activePath;
		void platforms.refresh();
	});

	$effect(() => {
		if (activePath === "/library") {
			selectedPlatform = page.url.searchParams.get("platform") ?? "";
			searchText = page.url.searchParams.get("q") ?? "";
			return;
		}

		selectedPlatform = "";
		searchText = "";
	});

	onMount(() => {
		const handleSessionExpired = () => {
			uploadQueue.cancel();
			setUserFont(null);
			// Someone opening a share link with an expired session comes back to it after signing in.
			void goto(`${resolve("/login")}${loginPathFor(page.url.pathname).slice("/login".length)}`, { replace: true });
		};
		window.addEventListener(
			"retroxide:session-expired",
			handleSessionExpired,
		);
		// Reloading or closing the tab loses the picked files, so the browser asks first.
		const warnBeforeLeaving = (event: BeforeUnloadEvent) => {
			if (uploadQueue.transferring) event.preventDefault();
		};
		window.addEventListener("beforeunload", warnBeforeLeaving);
		const refreshPlatforms = () => void platforms.refresh();
		window.addEventListener("focus", refreshPlatforms);
		// The account's language wins over the browser's, on every device the user signs in on.
		void i18n
			.initialize()
			.then(() => getProfile())
			.then((profile) => {
				isAdmin = profile.role === "admin";
				setUserFont(profile.font);
				return i18n.applyAccountLocale(profile.locale);
			})
			.catch((error: unknown) =>
				logger.warn("Unable to apply the account language.", error),
			);
		return () => {
			window.removeEventListener("beforeunload", warnBeforeLeaving);
			window.removeEventListener(
				"retroxide:session-expired",
				handleSessionExpired,
			);
			window.removeEventListener("focus", refreshPlatforms);
		};
	});
</script>

<div class="app-shell">
	<a class="skip-link" href="#app-shell-content"
		>{translate("app.shell.skip", "Skip to content")}</a
	>

	<header class="app-shell__header">
		<a
			class="app-shell__brand"
			href={resolve("/library")}
			aria-label={translate("nav.library", "Library")}
		>
			<img
				class="app-shell__brand-image app-shell__brand-image--main"
				src="/components/main-logo.png"
				alt={translate("app.shell.brand", "Retroxide")}
			/>
			<img
				class="app-shell__brand-image app-shell__brand-image--mini"
				src="/components/mini-logo.png"
				alt={translate("app.shell.brand", "Retroxide")}
			/>
		</a>

		<div
			class="app-shell__controls"
			aria-label={translate("app.shell.controls", "Application controls")}
		>
			<label
				class="app-shell__control-group"
				title={libraryControlsEnabled
					? undefined
					: translate(
							"app.shell.library_only",
							"Available on the Library page only.",
						)}
			>
				<span class="app-shell__assistive"
					>{translate("rom.fields.platform", "Platform")}</span
				>
				<span class="app-shell__control-icon" aria-hidden="true"
					><Gamepad2 size={15} strokeWidth={2.5} /></span
				>
				<select
					value={selectedPlatform}
					onchange={changePlatform}
					onfocus={() => void platforms.refresh()}
					aria-label={translate("rom.fields.platform", "Platform")}
					disabled={!libraryControlsEnabled}
					title={libraryControlsEnabled
						? undefined
						: translate(
								"app.shell.library_only",
								"Available on the Library page only.",
							)}
				>
					<option value=""
						>{translate("rom.fields.platform", "Platform")}</option
					>
					{#each platformOptions as platform (platform.value)}
						<option value={platform.value} selected={platform.value.toLowerCase() === selectedPlatform.toLowerCase()}>{platform.label}</option>
					{/each}
				</select>
			</label>

			<label
				class="app-shell__control-group app-shell__control-group--search"
				title={libraryControlsEnabled
					? undefined
					: translate(
							"app.shell.library_only",
							"Available on the Library page only.",
						)}
			>
				<span class="app-shell__assistive"
					>{translate(
						"app.shell.search_label",
						"Search library",
					)}</span
				>
				<span class="app-shell__control-icon" aria-hidden="true"
					><Search size={15} strokeWidth={2.5} /></span
				>
				<input
					type="search"
					value={searchText}
					oninput={changeSearch}
					placeholder={translate(
						"app.shell.search_placeholder",
						"Search",
					)}
					aria-label={translate(
						"app.shell.search_label",
						"Search library",
					)}
					disabled={!libraryControlsEnabled}
					title={libraryControlsEnabled
						? undefined
						: translate(
								"app.shell.library_only",
								"Available on the Library page only.",
							)}
				/>
			</label>

		</div>
	</header>

	<nav
		class="app-shell__nav"
		aria-label={translate("app.shell.navigation", "Primary navigation")}
	>
		<!-- The same links on every page; the current one is marked. -->
		{#each navLinks as link (link.path)}
			<a
				href={resolve(link.path)}
				class:current={inSection(link.path)}
				aria-current={activePath === link.path ? "page" : undefined}
				aria-label={link.path === "/uploads" && uploadQueue.transferring ? uploadsInProgress : undefined}
				title={link.path === "/uploads" && uploadQueue.transferring ? uploadsInProgress : undefined}
			>
				{link.label}
				{#if link.path === "/uploads" && uploadQueue.transferring}
					<span class="app-shell__nav-count" aria-hidden="true">{uploadQueue.transferringCount}</span>
				{/if}
			</a>
		{/each}
		<DropdownMenu.Root>
			<DropdownMenu.Trigger
				class={`app-shell__profile${isActive("/profile") || isActive("/users") ? " current" : ""}`}
				aria-label={translate("nav.profile", "Profile and settings")}
				title={translate("nav.profile", "Profile and settings")}
			>
				<img src="/components/btns/user.png" alt="" aria-hidden="true" />
			</DropdownMenu.Trigger>
			<DropdownMenu.Portal>
				<!-- preventScroll is off: locking the page would remove its scrollbar and shift it sideways. -->
				<DropdownMenu.Content class="app-shell__menu" align="end" sideOffset={6} collisionPadding={8} preventScroll={false}>
					<DropdownMenu.Item>
						{#snippet child({ props })}
							<a {...props} href={resolve("/profile")} aria-current={isActive("/profile") ? "page" : undefined}>
								{translate("nav.profile", "Profile and settings")}
							</a>
						{/snippet}
					</DropdownMenu.Item>
					{#if isAdmin}
						<DropdownMenu.Item>
							{#snippet child({ props })}
								<a {...props} href={resolve("/users")} aria-current={isActive("/users") ? "page" : undefined}>
									{translate("nav.users", "Users")}
								</a>
							{/snippet}
						</DropdownMenu.Item>
					{/if}
					<DropdownMenu.Item onSelect={() => void logout()} title={translate("common.actions.logoutHint", "Sign out on every device")}>
						{translate("common.actions.logout", "Log out")}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Portal>
		</DropdownMenu.Root>
	</nav>

	<main class="app-shell__content" id="app-shell-content" tabindex="-1">
		{@render children()}
	</main>
</div>
