<script lang="ts">
	import { onMount } from "svelte";
	import { activeRoute } from "@roxi/routify";
	import { ChevronLeft } from "@lucide/svelte";
	import { auth } from "../../lib/auth.svelte";
	import { requireAdmin } from "../../lib/guards";
	import { SETTINGS_TITLES } from "../../lib/settings-nav.svelte";
	import SettingsSidebar from "../../components/settings/SettingsSidebar.svelte";
	import ReadOnlyStrip from "../../components/settings/ReadOnlyStrip.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	// The user detail page (/settings/users/:id) renders full-width without the
	// settings sub-sidebar; the list and every other settings page keep the
	// shell. Track the resolved route via activeRoute — its `.url` is current
	// once navigation settles. (Reading window.location from an isActive emit
	// lagged one navigation behind: the shell stuck to the *previous* route, so
	// detail showed the shell and Back re-showed the bare list. See TopBar.)
	let pathname = $state(
		typeof window !== "undefined" ? window.location.pathname : "/",
	);
	onMount(() =>
		activeRoute.subscribe((r) => {
			if (r?.url) pathname = r.url.split("?")[0] ?? r.url;
		}),
	);
	let bare = $derived(/^\/settings\/users\/[^/]+$/.test(pathname));

	// /settings is the section list below lg; it is the top of the settings
	// tree, so it gets no Back-to-settings link of its own.
	let isIndex = $derived(pathname === "/settings" || pathname === "/settings/");
	let sectionTitle = $derived(SETTINGS_TITLES[pathname]?.() ?? "");
</script>

{#if bare}
	<!-- Routify renders the active route via Svelte-4 slot semantics; see routes/_module.svelte -->
	<!-- svelte-ignore slot_element_deprecated -->
	<slot />
{:else}
	<ReadOnlyStrip />
	<div
		class="mx-auto w-full max-w-7xl px-4 pb-0 pt-6 md:px-8 md:pt-7 lg:pb-7"
	>
		{#if !isIndex}
			<a
				href="/settings"
				class="touch-hit mb-3 inline-flex items-center gap-1.5 rounded-md py-1 pr-2 text-[13px] text-fg-muted transition hover:text-fg lg:hidden"
			>
				<ChevronLeft size={15} aria-hidden="true" />
				{i18n.nav_settings()}
				{#if sectionTitle}
					<span class="sr-only">— {sectionTitle}</span>
				{/if}
			</a>
		{/if}
		<div class="grid gap-5 lg:grid-cols-[220px_1fr] lg:gap-8">
			<SettingsSidebar />
			<section class="min-w-0">
				<!-- svelte-ignore slot_element_deprecated -->
				<slot />
			</section>
		</div>
	</div>
{/if}
