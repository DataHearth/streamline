<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "@lib/api";
	import { createAction, deleteFailed, refreshFailed, searchFailed, SERIES, updateFailed } from "@lib/actions";
	import type { TVShow, QualityProfile, SeriesType } from "@lib/types";
	import SeriesKebabMenu, { type SeriesAction } from "./SeriesKebabMenu.svelte";
	import QualityProfileModal from "@components/shared/QualityProfileModal.svelte";
	import SeriesTypeModal from "./SeriesTypeModal.svelte";
	import SeriesRenamePreviewModal from "./SeriesRenamePreviewModal.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import ReidentifyDialog from "@components/shared/ReidentifyDialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { show, variant = "card" }: { show: TVShow; variant?: "card" | "toolbar" } =
		$props();

	let hasFiles = $derived((show.have_episodes ?? 0) > 0);

	let qpOpen = $state(false);
	let typeOpen = $state(false);
	let renameOpen = $state(false);
	let deleteOpen = $state(false);
	let reidentifyOpen = $state(false);

	// Fetched once the quality dialog opens; the ["quality-profiles"] cache is
	// shared across every card so it resolves to a single request.
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: qpOpen,
	}));

	const saveProfile = createAction<string>(() => ({
		run: (profile) => api<TVShow>(`/series/${show.id}`, { method: "PATCH", body: { quality_profile: profile } }),
		refresh: SERIES,
		failed: updateFailed,
		succeeded: () => i18n.series_quality_updated(),
		then: () => (qpOpen = false),
	}));

	const saveType = createAction<SeriesType>(() => ({
		run: (type) => api<TVShow>(`/series/${show.id}`, { method: "PATCH", body: { type } }),
		refresh: SERIES,
		failed: updateFailed,
		succeeded: () => i18n.series_type_updated(),
		then: () => (typeOpen = false),
	}));

	const searchNow = createAction(() => ({
		run: () => api(`/series/${show.id}/search`, { method: "POST" }),
		refresh: [],
		failed: searchFailed,
		succeeded: () => i18n.series_search_dispatched_wanted(),
	}));

	const refresh = createAction(() => ({
		run: () => api(`/series/${show.id}/refresh-metadata`, { method: "POST" }),
		refresh: SERIES,
		failed: refreshFailed,
		succeeded: () => i18n.series_refresh_requested(),
	}));

	const del = createAction<boolean>(() => ({
		run: (withFiles) => api(`/series/${show.id}?delete_files=${withFiles}`, { method: "DELETE" }),
		refresh: SERIES,
		failed: deleteFailed,
		succeeded: () => i18n.series_deleted(),
		then: () => (deleteOpen = false),
	}));

	// Exhaustive by construction — SeriesKebabMenu owns the item list, so an
	// action this misses would render as a menu entry that does nothing.
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
	title={i18n.series_remove_title({ title: show.title })}
	body={i18n.series_remove_body()}
	filesLabel={i18n.series_delete_files_label()}
	filesNote={i18n.common_cannot_undo()}
	canDeleteFiles={hasFiles}
	pending={del.isPending}
	onClose={() => (deleteOpen = false)}
	onConfirm={(withFiles) => del.mutate(withFiles)}
/>
