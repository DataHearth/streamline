<script lang="ts">
	import { House, LogOut, ShieldX } from "@lucide/svelte";
	import AuthCard from "../components/auth/AuthCard.svelte";

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

<svelte:head><title>Forbidden — Streamline</title></svelte:head>

<AuthCard
	title="Access denied"
	subtitle="You don't have permission to view this page."
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
			href="/dashboard"
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<House size={16} aria-hidden="true" />
			Go to dashboard
		</a>
		<button
			type="button"
			onclick={signOut}
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-border-strong bg-surface px-4 text-sm font-medium text-fg transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<LogOut size={16} aria-hidden="true" />
			Sign out
		</button>
	</div>
</AuthCard>
