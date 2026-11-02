<script lang="ts">
	import { goto } from "$app/navigation";
	import { resolve } from "$app/paths";
	import { page } from "$app/state";
	import { Gamepad2, Search } from "@lucide/svelte";
	import { onMount } from "svelte";
	import type { Snippet } from "svelte";
	import { i18n, logger, logoutSession } from "$lib";
	import { useTranslate } from "$lib/i18n/helpers";

	type Props = {
		activePath: string;
		children: Snippet;
	};

	const platformOptions = [
		{ value: "nes", label: "NES" },
		{ value: "snes", label: "SNES" },
		{ value: "gba", label: "Game Boy Advance" },
		{ value: "gb", label: "Game Boy" },
		{ value: "gbc", label: "Game Boy Color" },
		{ value: "n64", label: "Nintendo 64" },
		{ value: "psx", label: "PlayStation" },
	];

	let { activePath, children }: Props = $props();

	let selectedPlatform = $state("");
	let searchText = $state("");

	const libraryControlsEnabled = $derived(activePath === "/library");
	const isLibrary = $derived(activePath === "/library");
	const translate = useTranslate(i18n);
	const routeLabel = $derived(
		activePath === "/downloads"
			? translate("nav.uploads", "Uploads")
			: activePath === "/uploads"
				? translate("nav.downloads", "Downloads")
				: activePath.startsWith("/rom/")
					? translate("nav.library", "Library")
					: "",
	);
	const backPath = $derived(
		activePath === "/downloads"
			? "/uploads"
			: activePath === "/uploads"
				? "/downloads"
				: "/library",
	);

	async function updateLibrarySearchParams(
		nextPlatform: string,
		nextQuery: string,
	): Promise<void> {
		if (activePath !== "/library") {
			return;
		}

		const params = new URLSearchParams(page.url.searchParams);
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
			replaceState: true,
			keepFocus: true,
			noScroll: true,
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

	function isActive(path: string): boolean {
		return activePath === path || activePath.startsWith(`${path}/`);
	}

	async function logout(): Promise<void> {
		try {
			await logoutSession();
		} catch (error) {
			logger.warn("Server logout failed; clearing the local session anyway.", error);
		}
		await goto(resolve("/login"), { replaceState: true });
	}

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
		const handleSessionExpired = () =>
			void goto(resolve("/login"), { replaceState: true });
		window.addEventListener(
			"retroxide:session-expired",
			handleSessionExpired,
		);
		void i18n
			.initialize()
			.catch((error: unknown) =>
				logger.warn("Unable to initialize translations.", error),
			);
		return () =>
			window.removeEventListener(
				"retroxide:session-expired",
				handleSessionExpired,
			);
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
						<option value={platform.value}>{platform.label}</option>
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
		{#if isLibrary}
			<a href={resolve("/downloads")}
				>{translate("nav.downloads", "Downloads")}</a
			>
			<a href={resolve("/uploads")}
				>{translate("nav.uploads", "Uploads")}</a
			>
			<button type="button" onclick={() => void logout()}>
				{translate("common.actions.logout", "Log out")}
			</button>
		{:else}
			<a class="app-shell__back" href={resolve(backPath)}>
				<img
					src="/components/btns/Back - 01.png"
					alt=""
					aria-hidden="true"
				/>
				<span>{routeLabel}</span>
			</a>
		{/if}
	</nav>

	<main class="app-shell__content" id="app-shell-content" tabindex="-1">
		{@render children()}
	</main>
</div>
