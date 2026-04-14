<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { Movie, QualityProfile } from "../../lib/types";
	import MovieKebabMenu from "./MovieKebabMenu.svelte";
	import QualityProfileModal from "./QualityProfileModal.svelte";
	import RenameMoviePreviewModal from "./RenameMoviePreviewModal.svelte";
	import Dialog from "../modals/Dialog.svelte";

	let { movie, variant = "card" }: { movie: Movie; variant?: "card" | "toolbar" } =
		$props();

	let hasFiles = $derived((movie.media_files?.length ?? 0) > 0);

	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let deleteWithFilesOpen = $state(false);

	const qc = useQueryClient();

	// Only fetched once the quality dialog is opened; the ["quality-profiles"]
	// cache is shared across every card so it resolves to a single request.
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: qpOpen,
	}));

	const searchNow = createMutation(() => ({
		mutationFn: () => api(`/movies/${movie.id}/search-now`, { method: "POST" }),
		onSuccess: () => toast.ok("Search dispatched"),
		onError: (e: Error) => toast.err(e.message ?? "Search failed"),
	}));

	const saveProfile = createMutation<Movie, Error, string>(() => ({
		mutationFn: (profile) =>
			api<Movie>(`/movies/${movie.id}`, {
				method: "PUT",
				body: { quality_profile: profile },
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["movie", movie.id] });
			qc.invalidateQueries({ queryKey: ["movies"] });
			toast.ok("Quality profile updated");
			qpOpen = false;
		},
		onError: (e: Error) => toast.err(e.message ?? "Update failed"),
	}));

	const refresh = createMutation(() => ({
		mutationFn: () =>
			api<Movie>(`/movies/${movie.id}/refresh-metadata`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["movie", movie.id] });
			toast.ok("Metadata refresh requested");
		},
		onError: (e: Error) => toast.err(e.message ?? "Refresh failed"),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/movies/${movie.id}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["movies"] });
			qc.invalidateQueries({ queryKey: ["movies", "counts"] });
			deleteOpen = false;
			deleteWithFilesOpen = false;
			toast.ok("Movie deleted");
		},
		onError: (e: Error) => toast.err(e.message ?? "Delete failed"),
	}));

	function onPick(a: string) {
		if (a === "search") searchNow.mutate();
		else if (a === "quality") qpOpen = true;
		else if (a === "rename") renameOpen = true;
		else if (a === "refresh") refresh.mutate();
		else if (a === "delete") deleteOpen = true;
		else if (a === "delete-with-files") deleteWithFilesOpen = true;
	}
</script>

<MovieKebabMenu
	{variant}
	{onPick}
	disabledActions={hasFiles ? [] : ["rename", "delete-with-files"]}
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
