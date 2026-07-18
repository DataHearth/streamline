<script lang="ts">
	import { onMount, tick, onDestroy } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { createQuery } from "@tanstack/svelte-query";
	import { activeRoute } from "@roxi/routify";
	import {
		Search,
		Plus,
		Film,
		Tv,
		FolderInput,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import type { SystemInfo } from "../../lib/types";
	import { toast } from "../../lib/toast";

	// Routify's `activeRoute` only emits once a navigation has resolved, so
	// its `.url` is always the page we're actually on. Reading
	// `window.location.pathname` from the `isActive` subscription instead
	// lagged one navigation behind (it fired before the URL updated).
	let pathname = $state(
		typeof window !== "undefined" ? window.location.pathname : "/",
	);
	onMount(() =>
		activeRoute.subscribe((r) => {
			if (r?.url) pathname = r.url.split("?")[0] ?? r.url;
		}),
	);

	type Crumb = { label: string; href?: string };
	// Sections that own their page heading (h1) and therefore want no title in
	// the topbar — only breadcrumbs appear when the user is on a detail page.
	const TITLELESS_PREFIXES = new Set(["/account", "/settings"]);
	const SECTIONS: { prefix: string; label: string }[] = [
		{ prefix: "/dashboard", label: "Dashboard" },
		{ prefix: "/movies", label: "Movies" },
		{ prefix: "/series", label: "Series" },
		{ prefix: "/activity", label: "Activity" },
		{ prefix: "/calendar", label: "Calendar" },
		{ prefix: "/requests", label: "Requests" },
		{ prefix: "/library/imports", label: "Imports" },
		{ prefix: "/account", label: "Account" },
		{ prefix: "/settings", label: "Settings" },
	];

	let crumbs = $derived.by<Crumb[]>(() => {
		const root = SECTIONS.find(
			(s) =>
				pathname === s.prefix || pathname.startsWith(s.prefix + "/"),
		);
		if (!root) return [];
		const rest = pathname.slice(root.prefix.length).replace(/^\//, "");
		if (!rest) {
			return TITLELESS_PREFIXES.has(root.prefix)
				? []
				: [{ label: root.label }];
		}
		return [{ label: root.label, href: root.prefix }, { label: "Details" }];
	});

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
		retry: false,
	}));

	type Health = "healthy" | "degraded" | "down";
	let health = $derived.by<Health>(() => {
		const info = systemQuery.data;
		if (!info) return "healthy";
		const kinds = [info.data_usage?.kind, info.db_usage?.kind];
		if (kinds.includes("err")) return "down";
		if (kinds.includes("warn") || info.https_warn) return "degraded";
		return "healthy";
	});

	function openPalette() {
		window.dispatchEvent(new CustomEvent("streamline:open-palette"));
	}
	function openAddMovie() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
	}
	function openAddSeries() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
	}

	// Add-to-library dropdown ------------------------------------------------
	// request_only users may only request, so they see a trimmed menu (Movie +
	// Series, which the modals route to a request) under a "Request a title"
	// heading. admins/members get the full "Add to library" menu.
	type AddItem = {
		id: "movie" | "import" | "series";
		label: string;
		desc: string;
		icon: typeof Film;
		soon?: boolean;
		divider?: boolean;
		requestable?: boolean;
	};
	const ADD_ITEMS: AddItem[] = [
		{
			id: "movie",
			label: "Movie",
			desc: "Search TMDB and start tracking",
			icon: Film,
			requestable: true,
		},
		{
			id: "series",
			label: "Series",
			desc: "TV, anime, daily shows",
			icon: Tv,
			requestable: true,
		},
		{
			id: "import",
			label: "Import existing files",
			desc: "Adopt media already on disk",
			icon: FolderInput,
			divider: true,
		},
	];

	let addHeading = $derived(
		auth.canAddDirectly ? "Add to library" : "Request a title",
	);
	// request_only only sees the requestable items (Movie / Series); Import is
	// admin-only, so members lose it too.
	let addItems = $derived.by(() => {
		const base = auth.canAddDirectly
			? ADD_ITEMS
			: ADD_ITEMS.filter((i) => i.requestable);
		return auth.isAdmin ? base : base.filter((i) => i.id !== "import");
	});

	let addOpen = $state(false);
	let addTrigger = $state<HTMLButtonElement | null>(null);
	let addMenu = $state<HTMLDivElement | null>(null);
	let addMenuTop = $state(0);
	let addMenuRight = $state(0);
	const ADD_MENU_W = 280;
	const ADD_MENU_GAP = 8;

	function recomputeAdd() {
		if (!addTrigger) return;
		const r = addTrigger.getBoundingClientRect();
		if (
			r.bottom < 0 ||
			r.top > window.innerHeight ||
			r.right < 0 ||
			r.left > window.innerWidth
		) {
			closeAdd();
			return;
		}
		addMenuTop = r.bottom + ADD_MENU_GAP;
		addMenuRight = Math.max(8, window.innerWidth - r.right);
	}

	async function openAddMenu() {
		addOpen = true;
		await tick();
		recomputeAdd();
		window.addEventListener("scroll", recomputeAdd, true);
		window.addEventListener("resize", recomputeAdd);
	}

	function closeAdd() {
		if (!addOpen) return;
		addOpen = false;
		window.removeEventListener("scroll", recomputeAdd, true);
		window.removeEventListener("resize", recomputeAdd);
	}

	function toggleAdd() {
		if (addOpen) closeAdd();
		else openAddMenu();
	}

	function pickAdd(item: AddItem) {
		closeAdd();
		if (item.soon) {
			toast.info(`${item.label}: not yet implemented`);
			return;
		}
		if (item.id === "movie") openAddMovie();
		else if (item.id === "series") openAddSeries();
		else if (item.id === "import") {
			window.location.href = "/library/imports";
		}
	}

	function onAddKey(e: KeyboardEvent) {
		if (!addOpen) return;
		if (e.key === "Escape") {
			e.preventDefault();
			closeAdd();
			addTrigger?.focus();
		}
	}
	function onAddDocClick(e: MouseEvent) {
		if (!addOpen) return;
		const t = e.target as Node;
		if (addMenu?.contains(t)) return;
		if (addTrigger?.contains(t)) return;
		closeAdd();
	}
	$effect(() => {
		if (addOpen) {
			document.addEventListener("mousedown", onAddDocClick);
			document.addEventListener("keydown", onAddKey);
			return () => {
				document.removeEventListener("mousedown", onAddDocClick);
				document.removeEventListener("keydown", onAddKey);
			};
		}
	});
	onDestroy(() => {
		window.removeEventListener("scroll", recomputeAdd, true);
		window.removeEventListener("resize", recomputeAdd);
	});

	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() {
				node.parentNode?.removeChild(node);
			},
		};
	}
</script>

<header
	class="sticky top-0 z-30 flex h-16 items-center gap-4 border-b border-border bg-bg-deep/70 px-4 backdrop-blur-md saturate-150 md:px-8"
>
	<div class="min-w-0 flex-1">
		{#if crumbs.length === 1}
			<h1 class="text-[22px] font-semibold leading-none tracking-tight text-fg">
				{crumbs[0]?.label}
			</h1>
		{:else if crumbs.length > 1}
			<nav
				aria-label="Breadcrumb"
				class="flex items-center gap-2 text-sm text-fg-muted"
			>
				{#each crumbs as c, i (i)}
					{#if c.href}
						<a
							href={c.href}
							class="transition hover:text-fg"
						>
							{c.label}
						</a>
					{:else}
						<span class="text-fg">{c.label}</span>
					{/if}
					{#if i < crumbs.length - 1}
						<span class="text-fg-faint" aria-hidden="true">/</span>
					{/if}
				{/each}
			</nav>
		{/if}
	</div>

	<button
		type="button"
		onclick={openPalette}
		aria-label="Open command palette"
		class="hidden h-10 w-full max-w-[540px] flex-1 shrink-0 items-center gap-2.5 rounded-md border border-border bg-surface px-3.5 text-left text-[13px] text-fg-subtle transition hover:border-border-strong hover:bg-surface-2 hover:text-fg-muted lg:flex"
	>
		<Search size={14} aria-hidden="true" />
		<span class="flex-1 truncate">Find a movie, release, indexer…</span>
		<kbd
			class="rounded border border-border bg-surface px-1.5 py-px font-mono text-[10.5px] text-fg-faint"
		>
			⌘ K
		</kbd>
	</button>

	<div class="flex flex-1 items-center justify-end gap-2">
		<button
			type="button"
			onclick={openPalette}
			aria-label="Search"
			class="grid h-10 w-10 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg lg:hidden"
		>
			<Search size={18} aria-hidden="true" />
		</button>
		<button
			bind:this={addTrigger}
			type="button"
			onclick={toggleAdd}
			aria-label={addHeading}
			aria-haspopup="menu"
			aria-expanded={addOpen}
			title={addHeading}
			class="grid h-10 w-10 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			<Plus size={18} aria-hidden="true" />
		</button>

		<div
			class={`health-pill health-${health} hidden items-center gap-2 rounded-full px-3 py-1.5 font-mono text-[11px] uppercase tracking-[0.08em] md:inline-flex`}
			title="System {health}"
		>
			<span aria-hidden="true" class="health-dot h-1.5 w-1.5 rounded-full"></span>
			<span>{health}</span>
		</div>
	</div>
</header>

{#if addOpen}
	<div
		bind:this={addMenu}
		use:portal
		role="menu"
		aria-label={addHeading}
		transition:fly={{ duration: 160, y: -4, easing: cubicOut }}
		class="add-menu fixed z-50 overflow-hidden rounded-md border border-border-strong bg-bg-elevated p-1 text-fg shadow-4"
		style:--menu-top="{addMenuTop}px"
		style:--menu-right="{addMenuRight}px"
		style:--menu-width="{ADD_MENU_W}px"
	>
		<div
			class="px-3 pb-1.5 pt-2 font-mono text-[9.5px] uppercase tracking-[0.18em] text-fg-faint"
		>
			{addHeading}
		</div>
		{#each addItems as item, i (item.id)}
			{#if item.divider && i > 0}
				<div class="-mx-1 my-1 h-px bg-border" role="separator"></div>
			{/if}
			<button
				role="menuitem"
				type="button"
				disabled={item.soon}
				onclick={() => pickAdd(item)}
				class={`flex w-full items-center gap-2.5 rounded-sm px-2.5 py-2 text-left transition-colors hover:bg-bg-hover focus-visible:bg-bg-hover focus-visible:outline-none ${
					item.soon ? "opacity-55 cursor-not-allowed hover:bg-transparent" : ""
				}`}
			>
				<span
					class="grid h-7 w-7 shrink-0 place-items-center rounded-sm bg-bg-card text-fg-muted"
				>
					<item.icon size={15} aria-hidden="true" />
				</span>
				<span class="flex min-w-0 flex-1 flex-col">
					<span class="text-[13px] font-medium leading-tight">
						{item.label}
					</span>
					<span class="mt-0.5 text-[10.5px] text-fg-subtle">
						{item.desc}
					</span>
				</span>
				{#if item.soon}
					<span
						class="rounded-sm border border-border px-1.5 py-px font-mono text-[9px] uppercase tracking-[0.1em] text-fg-faint"
					>
						soon
					</span>
				{/if}
			</button>
		{/each}
	</div>
{/if}

<style>
	.add-menu {
		top: var(--menu-top);
		right: var(--menu-right);
		width: var(--menu-width);
	}
	.health-pill.health-healthy {
		background-color: rgb(34 197 94 / 0.1);
		border: 1px solid rgb(34 197 94 / 0.22);
		color: var(--status-available);
	}
	.health-pill.health-degraded {
		background-color: rgb(245 158 11 / 0.1);
		border: 1px solid rgb(245 158 11 / 0.22);
		color: var(--status-wanted);
	}
	.health-pill.health-down {
		background-color: rgb(239 68 68 / 0.1);
		border: 1px solid rgb(239 68 68 / 0.22);
		color: var(--status-failed);
	}
	.health-pill.health-healthy .health-dot {
		background-color: var(--status-available);
		animation: health-pulse 2s var(--ease) infinite;
	}
	.health-pill.health-degraded .health-dot {
		background-color: var(--status-wanted);
	}
	.health-pill.health-down .health-dot {
		background-color: var(--status-failed);
	}
	@keyframes health-pulse {
		0% {
			box-shadow: 0 0 0 0 rgb(34 197 94 / 0.4);
		}
		70% {
			box-shadow: 0 0 0 6px rgb(34 197 94 / 0);
		}
		100% {
			box-shadow: 0 0 0 0 rgb(34 197 94 / 0);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.health-pill.health-healthy .health-dot {
			animation: none;
		}
	}
</style>
