<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { TVShow, QualityProfile } from "../../lib/types";
	import SeriesKebabMenu, { type SeriesAction } from "./SeriesKebabMenu.svelte";
	import QualityProfileModal from "../movies/QualityProfileModal.svelte";
	import SeriesRenamePreviewModal from "./SeriesRenamePreviewModal.svelte";
	import DeleteTitleDialog from "../shared/DeleteTitleDialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { show, variant = "card" }: { show: TVShow; variant?: "card" | "toolbar" } =
		$props();

	let hasFiles = $derived((show.have_episodes ?? 0) > 0);

	let qpOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);

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
		onError: (e: Error) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	const searchNow = createMutation(() => ({
		mutationFn: () => api(`/series/${show.id}/search`, { method: "POST" }),
		onSuccess: () => toast.ok("Search dispatched for wanted episodes"),
		onError: (e: Error) => toast.err(errorText(e, i18n.common_search_failed())),
	}));

	const refresh = createMutation(() => ({
		mutationFn: () =>
			api(`/series/${show.id}/refresh-metadata`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series", show.id] });
			toast.ok("Metadata refresh requested");
		},
		onError: (e: Error) => toast.err(errorText(e, i18n.common_refresh_failed())),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/series/${show.id}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series"] });
			deleteOpen = false;
			toast.ok("Series deleted");
		},
		onError: (e: Error) => toast.err(errorText(e, i18n.common_delete_failed())),
	}));

	function onPick(a: SeriesAction) {
		if (a === "search") searchNow.mutate();
		else if (a === "quality") qpOpen = true;
		else if (a === "rename") renameOpen = true;
		else if (a === "refresh") refresh.mutate();
		else if (a === "delete") deleteOpen = true;
	}
</script>

<SeriesKebabMenu
	{variant}
	{onPick}
	disabledActions={hasFiles ? [] : ["rename"]}
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

<DeleteTitleDialog
	open={deleteOpen}
	title="Remove '{show.title}' from your library?"
	body="The series leaves your library. Files on disk are kept unless you say otherwise."
	filesLabel="Also delete every downloaded episode from disk"
	filesNote="This cannot be undone."
	canDeleteFiles={hasFiles}
	pending={del.isPending}
	onClose={() => (deleteOpen = false)}
	onConfirm={(withFiles) => del.mutate(withFiles)}
/>
