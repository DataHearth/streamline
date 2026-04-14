<script lang="ts">
	import {
		createQuery,
		useQueryClient,
		createMutation,
	} from "@tanstack/svelte-query";
	import { params, goto } from "@roxi/routify";
	import { Search, Loader2, Bookmark } from "@lucide/svelte";
	import { onMount } from "svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import type { Movie, QualityProfile } from "../../lib/types";
	import MovieDetailHero from "../../components/movies/MovieDetailHero.svelte";
	import DetailAbout from "../../components/shared/DetailAbout.svelte";
	import MovieDetailInfo from "../../components/movies/MovieDetailInfo.svelte";
	import MovieDetailFiles from "../../components/movies/MovieDetailFiles.svelte";
	import MovieDetailHistory from "../../components/movies/MovieDetailHistory.svelte";
	import MovieDetailCast from "../../components/movies/MovieDetailCast.svelte";
	import MovieDetailSimilar from "../../components/movies/MovieDetailSimilar.svelte";
	import PlayOnMenu from "../../components/movies/PlayOnMenu.svelte";
	import MovieKebabMenu from "../../components/movies/MovieKebabMenu.svelte";
	import ManualSearchModal from "../../components/movies/ManualSearchModal.svelte";
	import QualityProfileModal from "../../components/movies/QualityProfileModal.svelte";
	import RenameMoviePreviewModal from "../../components/movies/RenameMoviePreviewModal.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";

	type Tab = "overview" | "files" | "history" | "cast";
	const TABS: { key: Tab; label: string }[] = [
		{ key: "overview", label: "Overview" },
		{ key: "files", label: "Files" },
		{ key: "history", label: "History" },
		{ key: "cast", label: "Cast" },
	];
	const VALID_TABS = new Set<Tab>(["overview", "files", "history", "cast"]);

	let routeParams = $state<Record<string, string>>({});
	let navigate = $state<(path: string) => void>(() => {});
	onMount(() => {
		const u1 = params.subscribe((p) => (routeParams = p));
		const u2 = goto.subscribe((fn) => (navigate = fn));
		return () => {
			u1();
			u2();
		};
	});
	const movieId = $derived(Number(routeParams.id));

	function readTab(): Tab {
		if (typeof window === "undefined") return "overview";
		const t = new URLSearchParams(window.location.search).get("tab");
		return t && VALID_TABS.has(t as Tab) ? (t as Tab) : "overview";
	}
	let tab = $state<Tab>(readTab());

	$effect(() => {
		if (typeof window === "undefined") return;
		const p = new URLSearchParams(window.location.search);
		if (tab === "overview") p.delete("tab");
		else p.set("tab", tab);
		const search = p.toString();
		const next = `${window.location.pathname}${search ? `?${search}` : ""}`;
		if (next !== window.location.pathname + window.location.search) {
			window.history.replaceState(null, "", next);
		}
	});

	const movieQuery = createQuery<Movie>(() => ({
		queryKey: ["movie", movieId],
		queryFn: () => api<Movie>(`/movies/${movieId}`),
		enabled: Number.isFinite(movieId) && movieId > 0,
	}));
	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
	}));

	let movie = $derived(movieQuery.data);
	let hasFiles = $derived((movie?.media_files?.length ?? 0) > 0);
	let qpName = $derived(movie?.quality_profile || "Server default");

	let searchOpen = $state(false);
	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let deleteWithFilesOpen = $state(false);

	const qc = useQueryClient();
	const refresh = createMutation(() => ({
		mutationFn: () =>
			api<Movie>(`/movies/${movieId}/refresh-metadata`, {
				method: "POST",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["movie", movieId] });
			toast.ok("Metadata refresh requested");
		},
		onError: (e: Error) => toast.err(e.message ?? "Refresh failed"),
	}));

	const searchNow = createMutation(() => ({
		mutationFn: () =>
			api(`/movies/${movieId}/search-now`, { method: "POST" }),
		onSuccess: () => toast.ok("Search dispatched"),
		onError: (e: Error) => toast.err(e.message ?? "Search failed"),
	}));

	const monitor = createMutation<Movie, Error, boolean>(() => ({
		mutationFn: (next) =>
			api<Movie>(`/movies/${movieId}`, {
				method: "PATCH",
				body: { monitored: next },
			}),
		onSuccess: (_d, next) => {
			qc.invalidateQueries({ queryKey: ["movie", movieId] });
			toast.ok(next ? "Now monitoring" : "Stopped monitoring");
		},
		onError: (e: Error) => toast.err(e.message ?? "Update failed"),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/movies/${movieId}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["movies"] });
			toast.ok("Movie deleted");
			navigate("/movies");
		},
		onError: (e: Error) => toast.err(e.message ?? "Delete failed"),
	}));

	function onKebabPick(a: string) {
		if (a === "search") searchNow.mutate();
		else if (a === "quality") qpOpen = true;
		else if (a === "rename") renameOpen = true;
		else if (a === "refresh") refresh.mutate();
		else if (a === "delete") deleteOpen = true;
		else if (a === "delete-with-files") deleteWithFilesOpen = true;
	}
</script>

{#if movieQuery.isLoading}
	<section class="relative overflow-hidden bg-bg-deep">
		<div
			class="flex w-full items-stretch gap-10 px-4 py-16 md:px-8"
		>
			<div
				class="aspect-[2/3] w-[200px] animate-pulse rounded-lg bg-bg-card/60 motion-reduce:animate-none"
			></div>
			<div class="flex flex-1 flex-col gap-3">
				<div
					class="h-8 w-2/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
				<div
					class="h-5 w-1/3 animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
				<div
					class="mt-auto h-24 w-full animate-pulse rounded bg-bg-card/60 motion-reduce:animate-none"
				></div>
			</div>
		</div>
	</section>
{:else if movieQuery.isError}
	<div
		class="mx-4 mt-4 rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center md:mx-8"
	>
		<p class="text-sm font-semibold text-status-failed">
			Failed to load movie
		</p>
		<p class="mt-1 text-xs text-fg-subtle">
			{movieQuery.error?.message ?? "Unknown error"}
		</p>
	</div>
{:else if movie}
	<MovieDetailHero {movie}>
		{#snippet actions()}
			{#if movie.status === "downloading"}
				<span
					class="inline-flex h-10 items-center gap-2 rounded-md bg-status-downloading/15 px-3 text-sm font-medium text-status-downloading"
				>
					<Loader2 size={14} class="animate-spin" aria-hidden="true" />
					Downloading…
				</span>
			{/if}

			<PlayOnMenu
				path={`/movies/${movie.id}/play-on`}
				queryKey={["movie", movie.id, "play-on"]}
				disabled={!hasFiles}
				disabledTitle="Available after the movie has been imported"
			/>

			<button
				type="button"
				onclick={() => (searchOpen = true)}
				class="inline-flex h-10 items-center gap-2 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover hover:shadow-glow"
			>
				<Search size={14} aria-hidden="true" />
				Manual search
			</button>

			<button
				type="button"
				onclick={() => monitor.mutate(!(movie.monitored ?? false))}
				disabled={monitor.isPending}
				aria-pressed={movie.monitored ?? false}
				title={movie.monitored ? "Stop monitoring" : "Monitor"}
				class="inline-flex h-10 w-10 items-center justify-center rounded-md border border-border-strong bg-white/[0.08] text-fg backdrop-blur-sm transition hover:bg-white/[0.14] disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Bookmark
					size={16}
					fill={movie.monitored ? "currentColor" : "none"}
					aria-hidden="true"
				/>
				<span class="sr-only">
					{movie.monitored ? "Stop monitoring" : "Monitor"}
				</span>
			</button>

			<MovieKebabMenu
				onPick={onKebabPick}
				disabledActions={hasFiles ? [] : ["rename", "delete-with-files"]}
			/>
		{/snippet}
	</MovieDetailHero>

	<nav
		aria-label="Movie sections"
		class="sticky top-14 z-10 border-b border-border bg-bg-deep/70 px-4 backdrop-blur-md saturate-150 md:px-8"
	>
		<div class="tabs-track flex w-full gap-0.5">
			{#each TABS as t (t.key)}
				{@const active = tab === t.key}
				<button
					type="button"
					onclick={() => (tab = t.key)}
					aria-current={active ? "page" : undefined}
					class={cn(
						"relative -mb-px shrink-0 px-4 py-3.5 text-[13px] font-medium transition",
						active ? "text-fg" : "text-fg-subtle hover:text-fg",
					)}
				>
					<span>{t.label}</span>
					{#if active}
						<span
							aria-hidden="true"
							class="absolute inset-x-3 -bottom-px h-0.5 rounded-t-sm bg-accent"
						></span>
					{/if}
				</button>
			{/each}
		</div>
	</nav>

	<div class="w-full px-4 py-6 md:px-8">
		{#if tab === "overview"}
			<div class="grid grid-cols-1 gap-6 lg:grid-cols-[1fr_320px] lg:gap-10">
				<DetailAbout
					overview={movie.overview}
					cast={movie.cast ?? []}
					onViewAllCast={() => (tab = "cast")}
				/>
				<MovieDetailInfo {movie} qualityProfileName={qpName} />
			</div>
		{:else if tab === "files"}
			<MovieDetailFiles files={movie.media_files ?? []} movieId={movie.id} />
		{:else if tab === "history"}
			<MovieDetailHistory movieId={movie.id} />
		{:else if tab === "cast"}
			<MovieDetailCast cast={movie.cast ?? []} />
		{/if}
	</div>

	<div class="w-full px-4 pb-12 md:px-8">
		<MovieDetailSimilar movieId={movie.id} />
	</div>

	<ManualSearchModal
		open={searchOpen}
		movieId={movie.id}
		onClose={() => (searchOpen = false)}
	/>
	<QualityProfileModal
		open={qpOpen}
		{movie}
		profiles={qpQuery.data ?? []}
		onClose={() => (qpOpen = false)}
	/>
	<RenameMoviePreviewModal
		open={renameOpen}
		movieId={movie.id}
		onClose={() => (renameOpen = false)}
	/>
	<Dialog
		open={deleteOpen}
		title="Remove '{movie.title}' from your library?"
		body="Files on disk will be kept."
		onClose={() => (deleteOpen = false)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label: "Delete",
				variant: "danger",
				dismiss: false,
				pending: del.isPending,
				onClick: () => del.mutate(false),
			},
		]}
	/>
	<Dialog
		open={deleteWithFilesOpen}
		title="Remove '{movie.title}' and delete its files?"
		body="{movie.media_files?.length ?? 0} file(s) will be deleted from disk. This cannot be undone."
		onClose={() => (deleteWithFilesOpen = false)}
		actions={[
			{ label: "Cancel", variant: "ghost", autofocus: true },
			{
				label: "Delete + files",
				variant: "danger",
				dismiss: false,
				pending: del.isPending,
				onClick: () => del.mutate(true),
			},
		]}
	/>
{/if}

<style>
	.tabs-track {
		overflow-x: auto;
		overflow-y: hidden;
		scrollbar-width: none;
	}
	.tabs-track::-webkit-scrollbar {
		display: none;
	}
</style>
