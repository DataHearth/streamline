<script lang="ts">
	import { House, LogOut, ShieldX } from "@lucide/svelte";
	import AuthCard from "../components/auth/AuthCard.svelte";
	import { m as i18n } from "../lib/paraglide/messages.js";

	async function signOut() {
		try {
			await fetch("/auth/logout", {
				method: "POST",
				credentials: "same-origin",
			});
		} catch {
			/* best-effort — cookie clear happens server-side */
		}
		window.location.assign("/login");
	}
</script>

<svelte:head><title>{i18n.error_403_page_title()}</title></svelte:head>

<AuthCard
	title={i18n.error_403_title()}
	subtitle={i18n.error_403_body()}
	eyebrow="403"
>
	<div class="mb-6 flex justify-center">
		<div
			class="grid h-14 w-14 place-items-center rounded-2xl bg-status-failed/10 text-status-failed"
		>
			<ShieldX size={28} aria-hidden="true" />
		</div>
	</div>

	<div class="flex flex-col gap-2">
		<a
			href="/"
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<House size={16} aria-hidden="true" />
			{i18n.error_go_to_dashboard()}
		</a>
		<button
			type="button"
			onclick={signOut}
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-border-strong bg-surface px-4 text-sm font-medium text-fg transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<LogOut size={16} aria-hidden="true" />
			{i18n.common_sign_out()}
		</button>
	</div>
</AuthCard>
