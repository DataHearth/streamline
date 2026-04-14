<script lang="ts">
	import { onMount, tick, type Component } from "svelte";
	import { goto } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import {
		Search,
		LayoutDashboard,
		Film,
		Tv,
		Inbox,
		Activity,
		FolderInput,
		CalendarDays,
		Settings,
		User,
		Users,
		LogOut,
		ArrowRight,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import { posterUrl, tvPosterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import type { Movie, TVShow } from "../../lib/types";

	type PageItem = {
		kind: "page";
		label: string;
		path: string;
		icon: Component;
	};
	type ActionItem = {
		kind: "action";
		label: string;
		icon: Component;
		run: () => void;
	};
	type MovieItem = {
		kind: "movie";
		id: number;
		label: string;
		year?: number;
	};
	type SeriesItem = {
		kind: "series";
		id: number;
		label: string;
		year?: number;
	};
	type Item = PageItem | ActionItem | MovieItem | SeriesItem;

	let open = $state(false);
	let closing = $state(false);
	let query = $state("");
	let cursor = $state(0);
	let inputEl = $state<HTMLInputElement | null>(null);
	let dialogEl = $state<HTMLDialogElement | null>(null);
	let prevFocus: HTMLElement | null = null;
	// Routify's goto resolves route PATTERNS (`/movies/[id]`), not concrete
	// paths — passing `/movies/1` fails with "could not travel to 1".
	let navigate!: (path: string, params?: Record<string, string>) => void;

	const moviesQuery = createQuery(() => ({
		queryKey: ["movies"],
		queryFn: () => api<{ items: Movie[] }>("/movies?page=1&limit=500"),
		staleTime: 30_000,
	}));

	const seriesQuery = createQuery(() => ({
		queryKey: ["series"],
		queryFn: () => api<{ items: TVShow[] }>("/series?page=1&limit=500"),
		staleTime: 30_000,
	}));

	const PAGES: PageItem[] = [
		{ kind: "page", label: "Dashboard", path: "/dashboard", icon: LayoutDashboard },
		{ kind: "page", label: "Movies", path: "/movies", icon: Film },
		{ kind: "page", label: "Series", path: "/series", icon: Tv },
		{ kind: "page", label: "Requests", path: "/requests", icon: Inbox },
		{ kind: "page", label: "Activity", path: "/activity", icon: Activity },
		{ kind: "page", label: "Imports", path: "/library/imports", icon: FolderInput },
		{ kind: "page", label: "Calendar", path: "/calendar", icon: CalendarDays },
		{ kind: "page", label: "Settings", path: "/settings", icon: Settings },
		{ kind: "page", label: "Account", path: "/account", icon: User },
	];

	function pages(): PageItem[] {
		const isAdmin = auth.user?.role === "admin";
		// Imports is admin-only.
		const base = PAGES.filter((p) => isAdmin || p.path !== "/library/imports");
		if (isAdmin) {
			base.push({
				kind: "page",
				label: "Users",
				path: "/settings/users",
				icon: Users,
			});
		}
		return base;
	}

	async function signOut() {
		try {
			await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
		} finally {
			window.location.href = "/login";
		}
	}

	function addMovie() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-movie"));
	}
	function addSeries() {
		window.dispatchEvent(new CustomEvent("streamline:open-add-series"));
	}

	// request_only users request rather than add, so the labels adapt.
	function actions(): ActionItem[] {
		const verb = auth.canAddDirectly ? "Add" : "Request";
		return [
			{ kind: "action", label: `${verb} movie…`, icon: Film, run: addMovie },
			{ kind: "action", label: `${verb} series…`, icon: Tv, run: addSeries },
			{ kind: "action", label: "Sign out", icon: LogOut, run: signOut },
		];
	}

	type Section = { label: string; items: Item[] };
	let sections = $derived.by<Section[]>(() => {
		const q = query.trim().toLowerCase();
		const matchedPages = pages().filter((p) =>
			p.label.toLowerCase().includes(q),
		);
		const matchedActions = actions().filter((a) =>
			a.label.toLowerCase().includes(q),
		);
		const out: Section[] = [];
		if (matchedPages.length)
			out.push({ label: "Pages", items: matchedPages });
		if (matchedActions.length)
			out.push({ label: "Quick actions", items: matchedActions });
		if (q.length >= 2 && moviesQuery.data) {
			const hits: MovieItem[] = moviesQuery.data.items
				.filter((m) => m.title.toLowerCase().includes(q))
				.slice(0, 5)
				.map((m) => ({
					kind: "movie",
					id: m.id,
					label: m.title,
					year: m.year,
				}));
			if (hits.length) out.push({ label: "Movies", items: hits });
		}
		if (q.length >= 2 && seriesQuery.data) {
			const hits: SeriesItem[] = seriesQuery.data.items
				.filter((s) => s.title.toLowerCase().includes(q))
				.slice(0, 5)
				.map((s) => ({
					kind: "series",
					id: s.id,
					label: s.title,
					year: s.year,
				}));
			if (hits.length) out.push({ label: "Series", items: hits });
		}
		return out;
	});

	let flat = $derived(sections.flatMap((s) => s.items));

	function indexOf(sectionIdx: number, itemIdx: number): number {
		const before = sections
			.slice(0, sectionIdx)
			.reduce((n, s) => n + s.items.length, 0);
		return before + itemIdx;
	}

	async function show() {
		if (open) return;
		prevFocus = document.activeElement as HTMLElement | null;
		open = true;
		query = "";
		cursor = 0;
		await tick();
		dialogEl?.showModal();
		await tick();
		inputEl?.focus();
	}

	function restoreFocus() {
		queueMicrotask(() => prevFocus?.focus());
	}

	function hide() {
		if (!open || closing) return;
		const d = dialogEl;
		if (!d?.open) {
			open = false;
			return;
		}
		// Reduced motion: close immediately, no exit transition to wait on.
		if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
			open = false;
			d.close();
			restoreFocus();
			return;
		}
		// Animate the exit by fading the box out (.is-closing) while the dialog
		// stays open, then close() on transitionend. close()-ing first and
		// relying on a display/overlay allow-discrete transition doesn't work
		// here — that discrete transition gets cancelled mid-flight.
		let timer = 0;
		const finish = () => {
			window.clearTimeout(timer);
			d.removeEventListener("transitionend", onEnd);
			closing = false;
			open = false;
			d.close();
			restoreFocus();
		};
		const onEnd = (e: TransitionEvent) => {
			if (e.target === d && e.propertyName === "opacity") finish();
		};
		closing = true;
		d.addEventListener("transitionend", onEnd);
		timer = window.setTimeout(finish, 220);
	}

	function onCancel(e: Event) {
		// Native Escape closes instantly with no exit animation — run ours.
		e.preventDefault();
		hide();
	}

	function activate(item: Item) {
		hide();
		if (item.kind === "page") navigate(item.path);
		else if (item.kind === "action") item.run();
		else if (item.kind === "movie")
			navigate("/movies/[id]", { id: String(item.id) });
		else if (item.kind === "series")
			navigate("/series/[id]", { id: String(item.id) });
	}

	function onKeydown(e: KeyboardEvent) {
		if (!open) {
			if ((e.key === "k" || e.key === "K") && (e.metaKey || e.ctrlKey)) {
				const active = document.activeElement as HTMLElement | null;
				const isInField =
					active &&
					(active.tagName === "INPUT" ||
						active.tagName === "TEXTAREA" ||
						active.isContentEditable);
				if (isInField) return;
				e.preventDefault();
				show();
			}
			return;
		}
		// Escape is left to the native <dialog>, which closes with the exit
		// animation and fires `close` → onclose={hide} for state cleanup.
		if (e.key === "ArrowDown") {
			e.preventDefault();
			if (flat.length === 0) return;
			cursor = (cursor + 1) % flat.length;
			scrollCursorIntoView();
		} else if (e.key === "ArrowUp") {
			e.preventDefault();
			if (flat.length === 0) return;
			cursor = (cursor - 1 + flat.length) % flat.length;
			scrollCursorIntoView();
		} else if (e.key === "Enter") {
			e.preventDefault();
			const item = flat[cursor];
			if (item) activate(item);
		}
	}

	function scrollCursorIntoView() {
		queueMicrotask(() => {
			dialogEl
				?.querySelector<HTMLElement>("[data-cmd-active]")
				?.scrollIntoView({ block: "nearest" });
		});
	}

	function onOpenEvent() {
		show();
	}

	function onBackdropClick(e: MouseEvent) {
		if (e.target === dialogEl) hide();
	}

	$effect(() => {
		// Reset cursor when query or sections shrink
		void query;
		void sections;
		cursor = 0;
	});

	onMount(() => {
		const unsubGoto = goto.subscribe((fn) => (navigate = fn));
		window.addEventListener("keydown", onKeydown);
		window.addEventListener("streamline:open-palette", onOpenEvent);
		return () => {
			unsubGoto();
			window.removeEventListener("keydown", onKeydown);
			window.removeEventListener("streamline:open-palette", onOpenEvent);
		};
	});
</script>

<dialog
	bind:this={dialogEl}
	aria-label="Command palette"
	class="palette max-h-[70dvh] w-[min(640px,92vw)] overflow-hidden rounded-xl border border-border-strong bg-bg-elevated text-fg shadow-4"
	class:is-closing={closing}
	onclick={onBackdropClick}
	oncancel={onCancel}
	onclose={hide}
>
	<div class="flex flex-col max-h-[70dvh]">
			<div
				class="search-row flex items-center gap-3 border-b border-border px-4 py-3"
			>
				<Search
					size={16}
					class="search-icon shrink-0 text-fg-subtle"
					aria-hidden="true"
				/>
				<input
					bind:this={inputEl}
					bind:value={query}
					type="text"
					placeholder="Search movies & series, run actions, jump anywhere…"
					class="search-input flex-1 border-0 bg-transparent text-[15px] text-fg placeholder:text-fg-faint"
					autocomplete="off"
					spellcheck="false"
				/>
				<kbd
					class="rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-[10px] text-fg-faint"
					>ESC</kbd
				>
			</div>

			<div class="flex-1 overflow-y-auto p-1.5">
				{#each sections as section, sIdx (section.label)}
					<div
						class="px-3 pt-2.5 pb-1 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
					>
						{section.label}
					</div>
					{#each section.items as item, iIdx (item.kind + "-" + iIdx)}
						{@const flatIdx = indexOf(sIdx, iIdx)}
						{@const active = flatIdx === cursor}
						<button
							type="button"
							onmouseenter={() => (cursor = flatIdx)}
							onclick={() => activate(item)}
							data-cmd-active={active ? "" : undefined}
							class={cn(
								"flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors",
								active
									? "bg-accent-soft text-fg"
									: "text-fg-muted hover:text-fg",
							)}
						>
							{#if item.kind === "movie" || item.kind === "series"}
								{@const MediaIcon = item.kind === "movie" ? Film : Tv}
								{@const poster =
									item.kind === "movie"
										? posterUrl({ id: item.id })
										: tvPosterUrl(item.id)}
								<div
									class="relative h-9 w-6 shrink-0 overflow-hidden rounded-md bg-surface-2"
								>
									<div
										class="absolute inset-0 grid place-items-center text-fg-muted"
									>
										<MediaIcon size={14} aria-hidden="true" />
									</div>
									<Poster
										src={poster}
										alt="{item.label} poster"
										class="relative h-full w-full object-cover"
									/>
								</div>
							{:else}
								{@const Icon = item.icon}
								<div
									class={cn(
										"grid h-7 w-7 shrink-0 place-items-center rounded-md transition-colors",
										active
											? "bg-accent text-fg-on-accent"
											: "bg-surface-2 text-fg-muted",
									)}
								>
									<Icon size={14} aria-hidden="true" />
								</div>
							{/if}
							<div class="min-w-0 flex-1">
								<div class="truncate text-[13px] font-medium">
									{item.label}
								</div>
								{#if (item.kind === "movie" || item.kind === "series") && item.year}
									<div
										class="truncate font-mono text-[10.5px] text-fg-subtle"
									>
										{item.year}
									</div>
								{/if}
							</div>
							<span
								class="font-mono text-[9.5px] uppercase tracking-[0.1em] text-fg-faint"
							>
								{item.kind === "page"
									? "Navigate"
									: item.kind === "action"
										? "Action"
										: item.kind === "movie"
											? "Movie"
											: "Series"}
							</span>
							{#if active}
								<ArrowRight
									size={12}
									class="shrink-0 text-fg-faint"
									aria-hidden="true"
								/>
							{/if}
						</button>
					{/each}
				{/each}

				{#if flat.length === 0}
					<div class="px-3 py-8 text-center text-[12.5px] text-fg-subtle">
						No matches for "{query}"
					</div>
				{/if}
			</div>

			<footer
				class="flex items-center gap-4 border-t border-border bg-bg-base px-4 py-2 font-mono text-[10.5px] text-fg-faint"
			>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↑</kbd
					><kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↓</kbd
					> navigate
				</span>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>↵</kbd
					> select
				</span>
				<span>
					<kbd
						class="mr-1 rounded border border-border bg-surface px-1 py-px text-fg-subtle"
						>Esc</kbd
					> close
				</span>
			</footer>
		</div>
</dialog>

<style>
	/* Open/close animation matches Modal.svelte (modalIn): opacity + 8px
	   translateY + 0.97→1 scale, with a faster exit.

	   Enter rides the native showModal() via @starting-style. The exit is
	   driven by JS (hide()): it adds .is-closing to fade the box *while the
	   dialog stays open*, then calls close() on transitionend. We deliberately
	   avoid display/overlay `allow-discrete` — that discrete transition starts
	   late and gets cancelled mid-flight in this app, killing the exit. */
	.palette {
		margin: 12vh auto 0;
		opacity: 0;
		transform: translateY(8px) scale(0.97);
	}
	.palette[open] {
		opacity: 1;
		transform: none;
		/* enter timing matches Modal's 180ms cubicOut */
		transition:
			opacity 180ms cubic-bezier(0.33, 1, 0.68, 1),
			transform 180ms cubic-bezier(0.33, 1, 0.68, 1);
	}
	@starting-style {
		.palette[open] {
			opacity: 0;
			transform: translateY(8px) scale(0.97);
		}
	}
	/* exit: faster (~67% of enter), ease-in */
	.palette[open].is-closing {
		opacity: 0;
		transform: translateY(8px) scale(0.97);
		transition:
			opacity 120ms cubic-bezier(0.32, 0, 0.67, 0),
			transform 120ms cubic-bezier(0.32, 0, 0.67, 0);
	}
	.palette::backdrop {
		background: rgb(2 2 3 / 0);
		backdrop-filter: blur(0);
	}
	.palette[open]::backdrop {
		background: rgb(2 2 3 / 0.6);
		backdrop-filter: blur(8px);
		transition:
			background 180ms ease-out,
			backdrop-filter 180ms ease-out;
	}
	@starting-style {
		.palette[open]::backdrop {
			background: rgb(2 2 3 / 0);
			backdrop-filter: blur(0);
		}
	}
	.palette[open].is-closing::backdrop {
		background: rgb(2 2 3 / 0);
		backdrop-filter: blur(0);
		transition:
			background 120ms ease-in,
			backdrop-filter 120ms ease-in;
	}
	@media (prefers-reduced-motion: reduce) {
		.palette[open],
		.palette[open].is-closing,
		.palette[open]::backdrop,
		.palette[open].is-closing::backdrop {
			transition: none;
		}
	}
	.search-row:focus-within {
		background: color-mix(in srgb, var(--accent) 4%, transparent);
	}
	.search-row:focus-within :global(.search-icon) {
		color: var(--accent);
	}
	.search-input:focus,
	.search-input:focus-visible {
		outline: none;
		box-shadow: none;
		border-radius: 0;
	}
</style>
