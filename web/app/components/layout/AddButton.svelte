<script lang="ts">
	import { onMount } from "svelte";
	import { Film, FolderInput, Plus, Tv } from "@lucide/svelte";
	import { activeRoute } from "@roxi/routify";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import { bulkMode } from "../../lib/bulk-mode.svelte";

	// Phone only. Adding is a repeated one-handed action, and the top-right
	// corner is the furthest point on the screen from a thumb — so the plus
	// leaves the bar below md and sits above the bottom nav instead, labelled,
	// which the 40px icon never was.
	//
	// It rides the route rather than the shell: the library screens and the
	// dashboard are where adding follows from what you're looking at. On a
	// detail page the action is about that one title, and Settings has nothing
	// to add to.
	const ROUTES = ["/dashboard", "/movies", "/series", "/requests"];

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
			if (!r?.url) return;
			const next = r.url.split("?")[0] ?? r.url;
			if (resolved && next !== pathname) open = false;
			resolved = true;
			pathname = next;
		}),
	);

	let onRoute = $derived(ROUTES.includes(pathname.replace(/\/+$/, "") || "/"));
	// A bulk selection owns the bottom of the screen; the pill would land on top
	// of BulkTouchBar's buttons.
	let visible = $derived(onRoute && !bulkMode.active);

	$effect(() => {
		if (!visible) open = false;
	});

	// main's pb-16 clears the bottom bar, not a 52px pill floating above it —
	// without the extra padding the last poster row ends underneath it. AppShell
	// owns the rule; this just says when it applies.
	$effect(() => {
		if (!visible) return;
		document.body.dataset.addPill = "";
		return () => {
			delete document.body.dataset.addPill;
		};
	});

	let label = $derived(auth.canAddDirectly ? "Add" : "Request");
	let heading = $derived(
		auth.canAddDirectly ? "Add to library" : "Request a title",
	);

	type Item = { id: "movie" | "series" | "import"; label: string; icon: typeof Film };
	let items = $derived<Item[]>([
		{ id: "movie", label: "Movie", icon: Film },
		{ id: "series", label: "Series", icon: Tv },
		// Adopting files on disk is an admin operation, and not a request.
		...(auth.canAddDirectly && auth.isAdmin
			? [{ id: "import" as const, label: "Import existing files", icon: FolderInput }]
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
		aria-label="Close"
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
			aria-label={open ? "Close" : heading}
			class={cn(
				"pill flex h-[52px] items-center justify-center rounded-full text-[15px] font-semibold shadow-4",
				open ? "bg-surface-2 text-fg-muted" : "bg-accent text-fg-on-accent",
			)}
			class:open
		>
			<!-- A plus turned 45° is the close mark, so the icon morphs rather than
			     being swapped for an X. -->
			<Plus class="pill-icon" size={20} strokeWidth={2.4} aria-hidden="true" />
			<span class="pill-label">{label}</span>
		</button>
	</div>
{/if}

<style>
	/* Clears the bottom bar (56px + safe area) with 28px of air under the pill. */
	.add-pill {
		bottom: calc(env(safe-area-inset-bottom) + 5.25rem);
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
		gap: 8px;
		padding-left: 17px;
		padding-right: 20px;
		transition:
			gap 200ms var(--ease),
			padding-left 200ms var(--ease),
			padding-right 200ms var(--ease),
			background-color 160ms var(--ease),
			color 160ms var(--ease);
	}
	/* Collapses to a 52px circle: 16 + 20 (icon) + 16. */
	.pill.open {
		gap: 0;
		padding-left: 16px;
		padding-right: 16px;
	}
	.pill :global(.pill-icon) {
		flex: none;
		transition: transform 220ms var(--ease);
	}
	.pill.open :global(.pill-icon) {
		transform: rotate(45deg);
	}
	.pill-label {
		max-width: 6rem;
		overflow: hidden;
		white-space: nowrap;
		transition:
			max-width 200ms var(--ease),
			opacity 130ms var(--ease);
	}
	.pill.open .pill-label {
		max-width: 0;
		opacity: 0;
	}

	@media (prefers-reduced-motion: reduce) {
		.scrim,
		.scrim.on,
		.fan-item,
		.fan-item.on,
		.pill,
		.pill :global(.pill-icon),
		.pill-label {
			transition: none;
		}
	}
</style>
