<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { TVShow, QualityProfile } from "../../lib/types";
	import SeriesKebabMenu, { type SeriesAction } from "./SeriesKebabMenu.svelte";
	import QualityProfileModal from "../movies/QualityProfileModal.svelte";
	import SeriesRenamePreviewModal from "./SeriesRenamePreviewModal.svelte";
	import Dialog from "../modals/Dialog.svelte";

	let { show, variant = "card" }: { show: TVShow; variant?: "card" | "toolbar" } =
		$props();

	let hasFiles = $derived((show.have_episodes ?? 0) > 0);

	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let deleteWithFilesOpen = $state(false);

	const qc = useQueryClient();

	// Fetched once the quality dialog opens; the ["quality-profiles"] cache is
	// shared across every card so it resolves to a single request.
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: qpOpen,
	}));

	const saveProfile = createMutation<TVShow, Error, string>(() => ({
		mutationFn: (profile) =>
			api<TVShow>(`/series/${show.id}`, {
				method: "PATCH",
				body: { quality_profile: profile },
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series", show.id] });
			qc.invalidateQueries({ queryKey: ["series"] });
			toast.ok("Quality profile updated");
			qpOpen = false;
		},
		onError: (e: Error) => toast.err(e.message ?? "Update failed"),
	}));

	const searchNow = createMutation(() => ({
		mutationFn: () => api(`/series/${show.id}/search`, { method: "POST" }),
		onSuccess: () => toast.ok("Search dispatched for wanted episodes"),
		onError: (e: Error) => toast.err(e.message ?? "Search failed"),
	}));

	const refresh = createMutation(() => ({
		mutationFn: () =>
			api(`/series/${show.id}/refresh-metadata`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series", show.id] });
			toast.ok("Metadata refresh requested");
		},
		onError: (e: Error) => toast.err(e.message ?? "Refresh failed"),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/series/${show.id}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series"] });
			deleteOpen = false;
			deleteWithFilesOpen = false;
			toast.ok("Series deleted");
		},
		onError: (e: Error) => toast.err(e.message ?? "Delete failed"),
	}));

	function onPick(a: SeriesAction) {
		if (a === "search") searchNow.mutate();
		else if (a === "quality") qpOpen = true;
		else if (a === "rename") renameOpen = true;
		else if (a === "refresh") refresh.mutate();
		else if (a === "delete") deleteOpen = true;
		else if (a === "delete-with-files") deleteWithFilesOpen = true;
	}
</script>

<SeriesKebabMenu
	{variant}
	{onPick}
	disabledActions={hasFiles ? [] : ["rename", "delete-with-files"]}
/>

<QualityProfileModal
	open={qpOpen}
	current={show.quality_profile}
	profiles={profilesQuery.data ?? []}
	saving={saveProfile.isPending}
	onClose={() => (qpOpen = false)}
	onSave={(p) => saveProfile.mutate(p)}
/>

<SeriesRenamePreviewModal
	open={renameOpen}
	seriesId={show.id}
	onClose={() => (renameOpen = false)}
/>

<Dialog
	open={deleteOpen}
	title="Remove '{show.title}' from your library?"
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
	title="Remove '{show.title}' and delete its files?"
	body="All downloaded episode files will be deleted from disk. This cannot be undone."
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
