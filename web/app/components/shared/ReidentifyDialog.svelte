<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type {
		ReidentifyResult,
		SeriesLookupResult,
		TMDBMovieResult,
	} from "../../lib/types";
	import AddMovieModal from "../movies/AddMovieModal.svelte";
	import AddSeriesModal from "../series/AddSeriesModal.svelte";
	import Dialog from "../modals/Dialog.svelte";

	type Props = {
		open: boolean;
		kind: "movie" | "series";
		id: number;
		currentTitle: string;
		onClose: () => void;
	};
	let { open, kind, id, currentTitle, onClose }: Props = $props();

	// The provider pick, held between the picker closing and the confirm being
	// answered. Confirming a destructive-ish repair on the click that chose the
	// row would make a mis-click unrecoverable.
	let picked = $state<{ id: number; title: string; year?: number } | null>(
		null,
	);

	$effect(() => {
		if (!open) picked = null;
	});

	const qc = useQueryClient();

	const reidentify = createMutation<ReidentifyResult, Error, number>(() => ({
		mutationFn: (providerId) =>
			api<ReidentifyResult>(`/${kind === "movie" ? "movies" : "series"}/${id}/reidentify`, {
				method: "POST",
				body:
					kind === "movie" ? { tmdb_id: providerId } : { tvdb_id: providerId },
			}),
		onSuccess: (res) => {
			const key = kind === "movie" ? "movie" : "series";
			qc.invalidateQueries({ queryKey: [key, id] });
			qc.invalidateQueries({ queryKey: [kind === "movie" ? "movies" : "series"] });
			qc.invalidateQueries({ queryKey: ["activity"] });
			const unmatched = res.unmatched?.length ?? 0;
			if (unmatched > 0) {
				// Not a failure, but the operator has files sitting outside the
				// library now and nothing else will tell them.
				toast.err(
					`Now "${res.title}". ${unmatched} file${unmatched === 1 ? "" : "s"} had no matching episode and were left on disk.`,
				);
			} else {
				toast.ok(
					res.renamed > 0
						? `Now "${res.title}" — ${res.renamed} file${res.renamed === 1 ? "" : "s"} renamed.`
						: `Now "${res.title}".`,
				);
			}
			picked = null;
			onClose();
		},
		onError: (e) => toast.err(errorText(e, "Could not change the match")),
	}));

	function onPickMovie(r: TMDBMovieResult) {
		picked = { id: r.tmdb_id, title: r.title, year: r.year };
	}
	function onPickSeries(r: SeriesLookupResult) {
		picked = { id: r.tvdb_id, title: r.title, year: r.year };
	}

	let pickedLabel = $derived(
		picked ? `${picked.title}${picked.year ? ` (${picked.year})` : ""}` : "",
	);
	let confirmBody = $derived(
		kind === "movie"
			? `"${currentTitle}" keeps its files, history and requests — only its TMDB identity changes. Metadata is refreshed and the files are renamed into the new title's folder.`
			: `"${currentTitle}" keeps its files and history. The season and episode list is replaced from TVDB, and each file is re-attached to the episode with the same season and episode number. Files with no counterpart in the new show are left on disk and reported.`,
	);
</script>

<!-- The picker is the add-flow's existing TMDB/TVDB selector in pick mode: same
     search, same detail panel, no library write. -->
{#if kind === "movie"}
	<AddMovieModal
		open={open && picked === null}
		mode="pick"
		seedQuery={currentTitle}
		onPick={onPickMovie}
		onClose={onClose}
	/>
{:else}
	<AddSeriesModal
		open={open && picked === null}
		mode="pick"
		seedQuery={currentTitle}
		onPick={onPickSeries}
		onClose={onClose}
	/>
{/if}

<Dialog
	open={open && picked !== null}
	title="Change the match to “{pickedLabel}”?"
	body={confirmBody}
	actions={[
		{ label: "Back", variant: "ghost", onClick: () => (picked = null) },
		{
			label: "Change match",
			variant: "primary",
			autofocus: true,
			pending: reidentify.isPending,
			disabled: reidentify.isPending,
			dismiss: false,
			onClick: () => picked && reidentify.mutate(picked.id),
		},
	]}
	onClose={() => (picked = null)}
/>
