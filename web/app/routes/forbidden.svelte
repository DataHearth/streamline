<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import { House, LogOut, ShieldX } from "@lucide/svelte";
	import AuthCard from "@components/auth/AuthCard.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

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
		<Button
			class="h-11 w-full"
			href="/"
		>
			<House size={16} aria-hidden="true" />
			{i18n.error_go_to_dashboard()}
		</Button>
		<Button
			variant="secondary"
			class="h-11 w-full"
			type="button"
			onclick={signOut}
		>
			<LogOut size={16} aria-hidden="true" />
			{i18n.common_sign_out()}
		</Button>
	</div>
</AuthCard>
