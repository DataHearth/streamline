<script lang="ts">
	import { onMount } from "svelte";
	import { Film, FolderInput, Plus, Tv } from "@lucide/svelte";
	import { activeRoute } from "@roxi/routify";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import { bulkMode } from "../../lib/bulk-mode.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Phone only. Adding is a repeated one-handed action, and the top-right
	// corner is the furthest point on the screen from a thumb — so the plus
	// leaves the bar below md and sits above the bottom nav instead, as a 52px
	// circle: the fan it opens names each destination, so the trigger doesn't
	// need to name itself.
	//
	// It rides the route rather than the shell: the dashboard and the two library
	// screens are where adding follows from what you're looking at, for everyone.
	// On a detail page the action is about that one title, and Settings has
	// nothing to add to.
	//
	// The two section pages belong to whoever acts on them. Imports is admin-only
	// — the route itself is behind `requireAdmin`, so below admin the pill would
	// have no audience there. Requests is the reverse: it is where a member or a
	// request_only member goes to ask for something, while for an admin it is a
	// queue of other people's asks and the work there is deciding them.
	const OPEN_ROUTES = ["/", "/movies", "/series"];

	let pathname = $state(
		typeof window !== "undefined" ? window.location.pathname : "/",
	);
	let open = $state(false);
	// Navigating closes the fan, and it closes here rather than in an effect on
	// `pathname` so that routify's first emit — which resolves the current URL
	// rather than leaving it — can't shut a fan the user just opened. Closing on
	// navigation also means the scrim is down before `visible` can flip and tear
	// its block out from under it.
	let resolved = false;
	onMount(() =>
		activeRoute.subscribe((r) => {
			// Routify reports the root index route's url as "" — a truthy guard
			// would leave the previous page's path in place when navigating home.
			if (!r) return;
			const next = (r.url ?? "").split("?")[0] || "/";
			if (resolved && next !== pathname) open = false;
			resolved = true;
			pathname = next;
		}),
	);

	let route = $derived(pathname.replace(/\/+$/, "") || "/");
	let onRoute = $derived(
		OPEN_ROUTES.includes(route) ||
			(route === "/library/imports" && auth.isAdmin) ||
			(route === "/requests" && !auth.isAdmin),
	);
	// A bulk selection owns the bottom of the screen; the pill would land on top
	// of BulkTouchBar's buttons.
	let visible = $derived(onRoute && !bulkMode.active);

	$effect(() => {
		if (!visible) open = false;
	});

	let heading = $derived(
		auth.canAddDirectly ? i18n.action_add_to_library() : i18n.action_request_title(),
	);

	type Item = { id: "movie" | "series" | "import"; label: string; icon: typeof Film };
	let items = $derived<Item[]>([
		{ id: "movie", label: i18n.common_movie(), icon: Film },
		{ id: "series", label: i18n.series_label(), icon: Tv },
		// Adopting files on disk is an admin operation, and not a request. On the
		// imports list itself the row would only lead back to the page it was
		// tapped from, and that page carries its own New scan button.
		...(auth.canAddDirectly && auth.isAdmin && route !== "/library/imports"
			? [{ id: "import" as const, label: i18n.add_import_existing(), icon: FolderInput }]
			: []),
	]);
	// Nearest the thumb wins: Movie sits directly above the pill.
	let fan = $derived([...items].reverse());

	function pick(id: Item["id"]) {
		open = false;
		if (id === "movie")
			window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
		else if (id === "series")
			window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
	}
</script>

{#if visible}
	<!-- Scrim and rows stay mounted and animate on classes rather than being
	     created and destroyed. A Svelte outro here can be cut short by `visible`
	     flipping on navigation, which leaves the node behind — that is what
	     orphaned the scrim before. CSS can't orphan anything. -->
	<button
		type="button"
		aria-label={i18n.common_close()}
		onclick={() => (open = false)}
		tabindex={open ? 0 : -1}
		aria-hidden={!open}
		class="scrim fixed inset-0 z-40 h-full w-full cursor-default bg-black/55 md:hidden"
		class:on={open}
	></button>

	<div
		class="add-pill fixed right-4 z-[45] flex flex-col items-end gap-2.5 md:hidden"
		role="group"
		aria-label={heading}
	>
		{#each fan as item, i (item.id)}
			{#if item.id === "import"}
				<a
					href="/library/imports"
					onclick={() => (open = false)}
					class="fan-item flex items-center gap-3"
					class:on={open}
					style="--din:{(fan.length - 1 - i) * 35}ms; --dout:{i * 25}ms"
					tabindex={open ? 0 : -1}
					aria-hidden={!open}
				>
					<span
						class="rounded-full border border-border-strong bg-bg-elevated px-3.5 py-2 text-[14px] font-medium leading-none text-fg shadow-3"
					>
						{item.label}
					</span>
					<span
						class="grid h-12 w-12 place-items-center rounded-full border border-border-strong bg-bg-elevated text-fg shadow-3"
					>
						<item.icon size={19} aria-hidden="true" />
					</span>
				</a>
			{:else}
				<button
					type="button"
					onclick={() => pick(item.id)}
					class="fan-item flex items-center gap-3"
					class:on={open}
					style="--din:{(fan.length - 1 - i) * 35}ms; --dout:{i * 25}ms"
					tabindex={open ? 0 : -1}
					aria-hidden={!open}
				>
					<span
						class="rounded-full border border-border-strong bg-bg-elevated px-3.5 py-2 text-[14px] font-medium leading-none text-fg shadow-3"
					>
						{item.label}
					</span>
					<span
						class="grid h-12 w-12 place-items-center rounded-full border border-border-strong bg-bg-elevated text-fg shadow-3"
					>
						<item.icon size={19} aria-hidden="true" />
					</span>
				</button>
			{/if}
		{/each}

		<button
			type="button"
			onclick={() => (open = !open)}
			aria-haspopup="menu"
			aria-expanded={open}
			aria-label={open ? i18n.common_close() : heading}
			class={cn(
				"pill grid h-[52px] w-[52px] place-items-center rounded-full shadow-4",
				open ? "bg-surface-2 text-fg-muted" : "bg-accent text-fg-on-accent",
			)}
			class:open
		>
			<!-- A plus turned 45° is the close mark, so the icon morphs rather than
			     being swapped for an X. -->
			<Plus class="pill-icon" size={20} strokeWidth={2.4} aria-hidden="true" />
		</button>
	</div>
{/if}

<style>
	/* Clears the bottom bar (56px + safe area) with 14px of air under the pill.
	   A plain 52px circle can sit closer to the bar than the old labelled pill. */
	.add-pill {
		bottom: calc(env(safe-area-inset-bottom) + 4.75rem);
		/* The closed fan still occupies its rows (visibility, not display), so the
		   column's box reaches most of the way up the screen. Without this it
		   swallows every tap over the grid behind it. Only the pill and an open
		   fan row take pointer events back. */
		pointer-events: none;
	}
	.add-pill > .pill {
		pointer-events: auto;
	}

	.scrim {
		opacity: 0;
		visibility: hidden;
		transition:
			opacity 130ms var(--ease),
			visibility 130ms;
	}
	.scrim.on {
		opacity: 1;
		visibility: visible;
		transition-duration: 170ms;
	}

	/* Rows rise from the pill, nearest first, and leave in the same order they
	   arrived — farthest out first, so the column collapses towards the thumb. */
	.fan-item {
		opacity: 0;
		visibility: hidden;
		pointer-events: none;
		transform: translateY(12px) scale(0.96);
		transform-origin: right center;
		transition:
			opacity 140ms var(--ease),
			transform 140ms var(--ease),
			visibility 140ms;
		transition-delay: var(--dout);
	}
	.fan-item.on {
		opacity: 1;
		visibility: visible;
		pointer-events: auto;
		transform: none;
		transition-duration: 190ms;
		transition-delay: var(--din);
	}

	.pill {
		transition:
			background-color 160ms var(--ease),
			color 160ms var(--ease);
	}
	.pill :global(.pill-icon) {
		flex: none;
		transition: transform 220ms var(--ease);
	}
	.pill.open :global(.pill-icon) {
		transform: rotate(45deg);
	}

	@media (prefers-reduced-motion: reduce) {
		.scrim,
		.scrim.on,
		.fan-item,
		.fan-item.on,
		.pill,
		.pill :global(.pill-icon) {
			transition: none;
		}
	}
</style>
