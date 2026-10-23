<script lang="ts">
	import { KeyRound, ShieldX } from "@lucide/svelte";
	import { auth } from "@lib/auth.svelte";
	import type { HardcoverIssue } from "@lib/music-books-lookup";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a book search shows when Hardcover cannot answer it: no key stored,
	// or a key Hardcover refuses. Both send an admin to the one field that fixes
	// it; everyone else is told who can. A saved key is read at startup, as the
	// TMDB and TVDB keys are, so the admin is told that too: without it, a new
	// key that has not been picked up yet reads as a fix that did not work.
	let { reason = "unset", onNavigate }: { reason?: HardcoverIssue; onNavigate?: () => void } = $props();
	let isAdmin = $derived(auth.user?.role === "admin");
	let rejected = $derived(reason === "rejected");
</script>

<div
	role="alert"
	class={rejected
		? "flex flex-col items-center rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 px-6 py-8 text-center"
		: "flex flex-col items-center rounded-lg border border-dashed border-status-wanted/40 bg-status-wanted/5 px-6 py-8 text-center"}
>
	{#if rejected}
		<ShieldX size={22} class="text-status-failed" aria-hidden="true" />
	{:else}
		<KeyRound size={22} class="text-status-wanted" aria-hidden="true" />
	{/if}
	<p class="mt-3 text-sm font-medium text-fg">
		{rejected ? i18n.books_lookup_key_rejected() : i18n.books_lookup_needs_key()}
	</p>
	{#if isAdmin}
		<a
			href="/settings/metadata#hardcover"
			onclick={() => {
				// The settings page focuses the field it is sent to; a client-side
				// navigation does not reliably carry the fragment, so say it here too.
				sessionStorage.setItem("streamline:focus-field", "hardcover");
				onNavigate?.();
			}}
			class="mt-3 inline-flex min-h-11 items-center rounded-md border border-border-strong px-3.5 text-sm font-medium text-fg transition hover:bg-surface lg:h-9 lg:min-h-0"
		>
			{rejected ? i18n.books_lookup_check_key() : i18n.books_lookup_add_key()}
		</a>
		<p class="mt-2 max-w-[34ch] text-xs text-fg-muted">{i18n.books_lookup_restart_hint()}</p>
	{:else}
		<p class="mt-1 text-xs text-fg-muted">
			{rejected ? i18n.books_lookup_ask_admin_check() : i18n.books_lookup_ask_admin()}
		</p>
	{/if}
</div>
