<script lang="ts">
	import { onMount } from "svelte";
	import { Compass, House, Film } from "@lucide/svelte";
	import AuthCard from "../components/auth/AuthCard.svelte";

	let path = $state("");
	onMount(() => {
		path = window.location.pathname;
	});
</script>

<svelte:head><title>Page not found — Streamline</title></svelte:head>

<AuthCard
	title="Off the grid"
	subtitle="The page you're looking for isn't streaming anywhere we can find."
	eyebrow="404"
>
	<div class="mb-6 flex justify-center">
		<div
			class="grid h-14 w-14 place-items-center rounded-2xl bg-accent-soft text-accent-text"
		>
			<Compass size={28} aria-hidden="true" />
		</div>
	</div>

	{#if path}
		<p
			class="mb-5 truncate rounded-md border border-border bg-bg-card px-3 py-2 text-center font-mono text-[11px] text-fg-muted"
			title={path}
		>
			{path}
		</p>
	{/if}

	<div class="flex flex-col gap-2">
		<a
			href="/dashboard"
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<House size={16} aria-hidden="true" />
			Back to dashboard
		</a>
		<a
			href="/movies"
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-border-strong bg-surface px-4 text-sm font-medium text-fg transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<Film size={16} aria-hidden="true" />
			Browse movies
		</a>
	</div>
</AuthCard>
