<script lang="ts">
	import { onMount } from "svelte";
	import { activeRoute } from "@roxi/routify";
	import { TriangleAlert } from "@lucide/svelte";
	import { auth } from "../../lib/auth.svelte";
	import { config } from "../../lib/config.svelte";
	import { requireAdmin } from "../../lib/guards";
	import SettingsSidebar from "../../components/settings/SettingsSidebar.svelte";

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
</script>

{#if bare}
	<!-- Routify renders the active route via Svelte-4 slot semantics; see routes/_module.svelte -->
	<!-- svelte-ignore slot_element_deprecated -->
	<slot />
{:else}
	<div class="mx-auto w-full max-w-7xl px-4 py-6 md:px-8 md:py-7">
		{#if config.readOnly}
			<div
				role="status"
				class="mb-5 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
			>
				<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<div>
					<p class="font-medium">Read-only configuration</p>
					<p class="mt-0.5 text-status-wanted/80">
						This instance is configured externally and runs read-only.
						Editing controls are disabled.
					</p>
				</div>
			</div>
		{/if}
		<div class="grid gap-5 md:grid-cols-[220px_1fr] md:gap-8">
			<SettingsSidebar />
			<section class="min-w-0">
				<!-- svelte-ignore slot_element_deprecated -->
				<slot />
			</section>
		</div>
	</div>
{/if}
