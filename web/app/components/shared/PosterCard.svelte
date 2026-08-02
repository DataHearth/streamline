<script lang="ts">
	import type { Snippet } from "svelte";
	import { Film, Search, Bookmark } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { posterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import StatusPill from "./StatusPill.svelte";
	import ProgressBar from "./ProgressBar.svelte";
	import SelectBox from "./SelectBox.svelte";
	import type { StatusKind } from "./StatusPill.svelte";

	type PosterMovie = {
		id: number;
		title: string;
		original_title?: string;
		year: number;
		status: StatusKind;
		monitored?: boolean;
		progress?: number;
		rating?: number;
		size_text?: string;
		resolution?: string;
	};

	let {
		movie,
		size = "md",
		showMeta = true,
		pillVariant = "solid",
		href,
		posterSrc,
		onMonitor,
		onSearch,
		selected = false,
		selectionActive = false,
		onSelect,
		kebab,
	}: {
		movie: PosterMovie;
		size?: "sm" | "md" | "lg";
		showMeta?: boolean;
		pillVariant?: "solid" | "translucent";
		// Default to the movie detail route + movie poster; series pass their own.
		href?: string;
		posterSrc?: string;
		onMonitor?: () => void;
		onSearch?: () => void;
		// Selection is opt-in: pass onSelect to make the card selectable. The
		// checkbox hides behind hover until something is selected, so ordinary
		// browsing keeps the poster art clean.
		selected?: boolean;
		selectionActive?: boolean;
		onSelect?: (v: boolean) => void;
		kebab?: Snippet;
	} = $props();

	let cardHref = $derived(href ?? `/movies/${movie.id}`);
	let cardPoster = $derived(posterSrc ?? posterUrl(movie));

	// While a selection is in progress the whole card becomes a selection
	// target — clicking through to a detail page mid-triage loses the set.
	function onCardClick(e: MouseEvent) {
		if (!onSelect || !selectionActive) return;
		e.preventDefault();
		onSelect(!selected);
	}

	function stop(handler?: (e: MouseEvent) => void) {
		return (e: MouseEvent) => {
			e.preventDefault();
			e.stopPropagation();
			handler?.(e);
		};
	}
</script>

<div
	id="poster-card-{movie.id}"
	class={cn(
		"group relative rounded-lg transition duration-200",
		"hover:scale-[1.02] hover:shadow-[0_0_0_2px_var(--accent-ring),0_24px_64px_rgb(0_0_0_/0.55)]",
		"focus-within:scale-[1.02] focus-within:shadow-[0_0_0_2px_var(--accent-ring),0_24px_64px_rgb(0_0_0_/0.55)]",
		size === "sm" && "w-[120px]",
		size === "md" && "w-full",
		size === "lg" && "w-[200px]",
		selected && "shadow-[0_0_0_2px_var(--accent),0_24px_64px_rgb(0_0_0_/0.55)]",
	)}
>
	<a
		href={cardHref}
		onclick={onCardClick}
		class="relative block aspect-[2/3] w-full overflow-hidden rounded-lg focus:outline-none"
	>
		<div class="absolute inset-0 bg-bg-card"></div>
		<div class="absolute inset-0 grid place-items-center text-fg-faint">
			<Film class="h-10 w-10" aria-hidden="true" />
		</div>
		<Poster
			src={cardPoster}
			alt="{movie.title} poster"
			class="relative h-full w-full object-cover"
		/>

		<div
			class="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/70 to-transparent px-3 pt-12 pb-2.5"
		>
			<p
				class="truncate text-sm font-semibold text-white drop-shadow-[0_1px_3px_rgb(0_0_0_/0.95)]"
				title={movie.title}
			>
				{movie.title}
			</p>
			{#if movie.original_title && movie.original_title.trim() && movie.original_title.trim() !== movie.title.trim()}
				<p
					class="truncate text-[11px] italic text-white/70 drop-shadow-[0_1px_2px_rgb(0_0_0_/0.9)]"
					title={movie.original_title}
				>
					{movie.original_title}
				</p>
			{/if}
			<p
				class="mt-0.5 font-mono text-[11px] tracking-tight text-white/80 drop-shadow-[0_1px_2px_rgb(0_0_0_/0.9)]"
			>
				{movie.year}{#if movie.rating && movie.rating > 0}
					<span class="text-white/70"> · ★ {movie.rating.toFixed(1)}</span>
				{/if}
			</p>
			{#if movie.resolution || movie.size_text}
				<p
					class="font-mono text-[11px] tracking-tight text-white/65 drop-shadow-[0_1px_2px_rgb(0_0_0_/0.9)]"
				>
					{[movie.resolution, movie.size_text].filter(Boolean).join(" · ")}
				</p>
			{/if}
		</div>

		<div
			class={cn(
				"absolute left-2 top-2 transition",
				onSelect &&
					(selectionActive
						? "opacity-0"
						: "group-hover:opacity-0 group-focus-within:opacity-0"),
			)}
		>
			<StatusPill
				status={movie.status}
				size="sm"
				live={movie.status === "downloading"}
				variant={pillVariant}
			/>
		</div>

		{#if movie.status === "downloading"}
			<div class="absolute inset-x-0 bottom-0">
				<ProgressBar
					value={movie.progress}
					status="downloading"
					height={2}
					label="Downloading"
				/>
			</div>
		{/if}
	</a>

	{#if onSelect}
		<div
			class={cn(
				"absolute left-2 top-2 z-10 transition",
				selectionActive
					? "opacity-100"
					: "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100",
			)}
		>
			<SelectBox
				variant="card"
				checked={selected}
				onChange={(v) => onSelect(v)}
				label={selected ? `Deselect ${movie.title}` : `Select ${movie.title}`}
			/>
		</div>
	{/if}

	{#if onMonitor}
		<button
			type="button"
			onclick={stop(onMonitor)}
			aria-label={movie.monitored ? "Stop monitoring" : "Monitor"}
			aria-pressed={movie.monitored ?? false}
			title={movie.monitored ? "Stop monitoring" : "Monitor"}
			class={cn(
				"absolute right-2 top-2 z-10 grid h-7 w-7 place-items-center rounded-full border border-white/10 bg-bg-deep transition hover:bg-bg-elevated focus:outline-none focus:ring-2 focus:ring-accent-ring",
				movie.monitored ? "text-accent-text" : "text-fg-subtle hover:text-fg",
			)}
		>
			<Bookmark
				size={14}
				fill={movie.monitored ? "currentColor" : "none"}
				aria-hidden="true"
			/>
		</button>
	{/if}

	<div
		class="pointer-events-none absolute right-2 bottom-2 flex translate-y-1 gap-1 opacity-0 transition duration-200 group-hover:pointer-events-auto group-hover:translate-y-0 group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:translate-y-0 group-focus-within:opacity-100"
	>
		{#if onSearch}
			<button
				type="button"
				onclick={stop(onSearch)}
				aria-label="Search releases"
				class="grid h-7 w-7 place-items-center rounded-full border border-white/10 bg-black/65 text-white backdrop-blur-sm transition hover:bg-black/80"
			>
				<Search size={14} aria-hidden="true" />
			</button>
		{/if}
		{#if kebab}
			{@render kebab()}
		{/if}
	</div>
</div>
