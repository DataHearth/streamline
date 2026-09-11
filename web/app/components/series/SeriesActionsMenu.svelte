<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { TVShow, QualityProfile, SeriesType } from "../../lib/types";
	import SeriesKebabMenu, { type SeriesAction } from "./SeriesKebabMenu.svelte";
	import QualityProfileModal from "../movies/QualityProfileModal.svelte";
	import SeriesTypeModal from "./SeriesTypeModal.svelte";
	import SeriesRenamePreviewModal from "./SeriesRenamePreviewModal.svelte";
	import DeleteTitleDialog from "../shared/DeleteTitleDialog.svelte";
	import ReidentifyDialog from "../shared/ReidentifyDialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { show, variant = "card" }: { show: TVShow; variant?: "card" | "toolbar" } =
		$props();

	let hasFiles = $derived((show.have_episodes ?? 0) > 0);

	let qpOpen = $state(false);
	let typeOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let reidentifyOpen = $state(false);

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

	const saveType = createMutation<TVShow, Error, SeriesType>(() => ({
		mutationFn: (t) =>
			api<TVShow>(`/series/${show.id}`, { method: "PATCH", body: { type: t } }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["series", show.id] });
			qc.invalidateQueries({ queryKey: ["series"] });
			toast.ok(i18n.series_type_updated());
			typeOpen = false;
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

	// Exhaustive by construction — SeriesKebabMenu owns the item list, so an
	// action this misses would render as a menu entry that does nothing.
	// "delete-files" never reaches here: it needs the loaded episode list, so
	// the card menu does not offer it (allowDeleteFiles defaults false).
	function onPick(a: SeriesAction) {
		switch (a) {
			case "search":
				searchNow.mutate();
				break;
			case "quality":
				qpOpen = true;
				break;
			case "type":
				typeOpen = true;
				break;
			case "rename":
				renameOpen = true;
				break;
			case "refresh":
				refresh.mutate();
				break;
			case "reidentify":
				reidentifyOpen = true;
				break;
			case "delete":
				deleteOpen = true;
				break;
			case "delete-files":
				break;
			default: {
				const unhandled: never = a;
				void unhandled;
			}
		}
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

<SeriesTypeModal
	open={typeOpen}
	current={show.type}
	saving={saveType.isPending}
	onClose={() => (typeOpen = false)}
	onSave={(t) => saveType.mutate(t)}
/>

<SeriesRenamePreviewModal
	open={renameOpen}
	seriesId={show.id}
	onClose={() => (renameOpen = false)}
/>

<ReidentifyDialog
	open={reidentifyOpen}
	kind="series"
	id={show.id}
	currentTitle={show.title}
	onClose={() => (reidentifyOpen = false)}
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
