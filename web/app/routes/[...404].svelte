<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import { onMount } from "svelte";
	import { Compass, House, Film } from "@lucide/svelte";
	import AuthCard from "@components/auth/AuthCard.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Legacy paths of torrents, transcoding and imports under /activity and
	// /library; bookmarks and an installed app's history still hold those URLs.
	const MOVED: [RegExp, string][] = [
		[/^\/activity\/torrents\/?$/, "/torrents"],
		[/^\/activity\/transcoding\/?$/, "/transcoding"],
		[/^\/library\/imports(\/.*)?$/, "/imports$1"],
	];

	function movedTo(pathname: string): string | null {
		for (const [from, to] of MOVED) {
			if (from.test(pathname)) return pathname.replace(from, to);
		}
		return null;
	}

	// Decided before the first render, so a moved URL never paints the 404 card.
	const { pathname, search, hash } = window.location;
	const target = movedTo(pathname);

	let path = $state("");
	onMount(() => {
		if (target) {
			window.location.replace(target + search + hash);
			return;
		}
		path = pathname;
	});
</script>

<svelte:head><title>{i18n.error_404_page_title()}</title></svelte:head>

{#if !target}
	<AuthCard
		title={i18n.error_404_title()}
		subtitle={i18n.error_404_body()}
		eyebrow="404"
		inShell
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
			<Button
				class="h-11 w-full"
				href="/"
			>
				<House size={16} aria-hidden="true" />
				{i18n.error_back_to_dashboard()}
			</Button>
			<Button
				variant="secondary"
				class="h-11 w-full"
				href="/movies"
			>
				<Film size={16} aria-hidden="true" />
				{i18n.error_browse_movies()}
			</Button>
		</div>
	</AuthCard>
{/if}
