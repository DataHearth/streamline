<script lang="ts">
	import { NOUN_FILE } from "@lib/nouns";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "@lib/api";
	import { createAction, deleteFailed, MOVIES, refreshFailed, searchFailed, updateFailed } from "@lib/actions";
	import type { Movie, QualityProfile } from "@lib/types";
	import MovieKebabMenu from "./MovieKebabMenu.svelte";
	import QualityProfileModal from "@components/shared/QualityProfileModal.svelte";
	import RenameMoviePreviewModal from "./RenameMoviePreviewModal.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import ReidentifyDialog from "@components/shared/ReidentifyDialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { movie, variant = "card" }: { movie: Movie; variant?: "card" | "toolbar" } =
		$props();

	// The menu opens from both a list row and the detail page, and the two
	// carry the file count differently: a list response has the rollup, a
	// detail response has the files themselves.
	let fileCount = $derived(
		movie.file_summary?.file_count ?? movie.media_files?.length ?? 0,
	);
	let hasFiles = $derived(fileCount > 0);

	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let reidentifyOpen = $state(false);

	// Only fetched once the quality dialog is opened; the ["quality-profiles"]
	// cache is shared across every card so it resolves to a single request.
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: qpOpen,
	}));

	const searchNow = createAction(() => ({
		run: () => api(`/movies/${movie.id}/search-now`, { method: "POST" }),
		refresh: [],
		failed: searchFailed,
		succeeded: () => i18n.movie_search_dispatched(),
	}));

	const saveProfile = createAction<string>(() => ({
		run: (profile) => api<Movie>(`/movies/${movie.id}`, { method: "PATCH", body: { quality_profile: profile } }),
		refresh: MOVIES,
		failed: updateFailed,
		succeeded: () => i18n.movie_quality_updated(),
		then: () => (qpOpen = false),
	}));

	const refresh = createAction(() => ({
		run: () => api<Movie>(`/movies/${movie.id}/refresh-metadata`, { method: "POST" }),
		refresh: MOVIES,
		failed: refreshFailed,
		succeeded: () => i18n.movie_refresh_requested(),
	}));

	const del = createAction<boolean>(() => ({
		run: (withFiles) => api(`/movies/${movie.id}?delete_files=${withFiles}`, { method: "DELETE" }),
		refresh: [["movies"]],
		failed: deleteFailed,
		succeeded: () => i18n.movie_deleted(),
		then: () => (deleteOpen = false),
	}));

	function onPick(a: string) {
		if (a === "search") searchNow.mutate();
		else if (a === "quality") qpOpen = true;
		else if (a === "rename") renameOpen = true;
		else if (a === "refresh") refresh.mutate();
		else if (a === "reidentify") reidentifyOpen = true;
		else if (a === "delete") deleteOpen = true;
	}
</script>

<MovieKebabMenu
	{variant}
	{onPick}
	disabledActions={hasFiles ? [] : ["rename"]}
/>

<QualityProfileModal
	open={qpOpen}
	current={movie.quality_profile}
	profiles={profilesQuery.data ?? []}
	saving={saveProfile.isPending}
	onClose={() => (qpOpen = false)}
	onSave={(p) => saveProfile.mutate(p)}
/>
<RenameMoviePreviewModal
	open={renameOpen}
	movieId={movie.id}
	onClose={() => (renameOpen = false)}
/>
<ReidentifyDialog
	open={reidentifyOpen}
	kind="movie"
	id={movie.id}
	currentTitle={movie.title}
	onClose={() => (reidentifyOpen = false)}
/>
<DeleteTitleDialog
	open={deleteOpen}
	title={i18n.movie_remove_title({ title: movie.title })}
	body={i18n.movie_remove_body()}
	filesLabel={i18n.bulk_delete_files_label({ items: NOUN_FILE.count(fileCount) })}
	filesNote={i18n.common_cannot_undo()}
	canDeleteFiles={fileCount > 0}
	pending={del.isPending}
	onClose={() => (deleteOpen = false)}
	onConfirm={(withFiles) => del.mutate(withFiles)}
/>
