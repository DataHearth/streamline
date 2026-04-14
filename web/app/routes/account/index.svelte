<script lang="ts">
	import IdentityHero from "../../components/account/IdentityHero.svelte";
	import ProfileCard from "../../components/account/ProfileCard.svelte";
	import PasswordCard from "../../components/account/PasswordCard.svelte";
	import APIKeysCard from "../../components/account/APIKeysCard.svelte";
	import SessionsCard from "../../components/account/SessionsCard.svelte";
	import JWTRotateCard from "../../components/account/JWTRotateCard.svelte";
	import { auth } from "../../lib/auth.svelte";

	let isAdmin = $derived(auth.user?.role === "admin");
</script>

<div
	class="mx-auto w-full max-w-[920px] space-y-8 px-4 py-6 md:px-6 md:py-7 lg:px-8"
>
	<IdentityHero />

	{@render section(
		"Identity",
		"Who you are and how you sign in.",
		identitySection,
	)}

	{@render section(
		"Devices & access",
		"Where you're signed in and which tools can talk to your account.",
		devicesSection,
	)}

	{#if isAdmin}
		{@render section(
			"Danger zone",
			"Destructive, server-wide actions. Admin only.",
			dangerSection,
			true,
		)}
	{/if}
</div>

{#snippet section(
	title: string,
	subtitle: string,
	body: import("svelte").Snippet,
	danger = false,
)}
	<section>
		<header
			class="mb-4 flex flex-wrap items-baseline gap-x-3 gap-y-1"
		>
			<h2
				class={[
					"font-mono text-[11px] font-semibold uppercase tracking-[0.16em]",
					danger ? "text-status-failed" : "text-fg-faint",
				]}
			>
				{title}
			</h2>
			<span
				class={[
					"hidden h-px flex-1 sm:block",
					danger ? "bg-status-failed/30" : "bg-border",
				]}
				aria-hidden="true"
			></span>
			<p class="text-xs text-fg-subtle">{subtitle}</p>
		</header>
		{@render body()}
	</section>
{/snippet}

{#snippet identitySection()}
	<div class="grid items-start gap-4 md:grid-cols-2">
		<ProfileCard />
		<PasswordCard />
	</div>
{/snippet}

{#snippet devicesSection()}
	<div class="grid items-start gap-4">
		<APIKeysCard />
		<SessionsCard />
	</div>
{/snippet}

{#snippet dangerSection()}
	<JWTRotateCard />
{/snippet}
