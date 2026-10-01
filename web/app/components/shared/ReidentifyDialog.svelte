<script lang="ts">
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type {
		ReidentifyResult,
		SeriesLookupResult,
		TMDBMovieResult,
	} from "@lib/types";
	import AddMovieModal from "@components/movies/AddMovieModal.svelte";
	import AddSeriesModal from "@components/series/AddSeriesModal.svelte";
	import Dialog from "@components/modals/Dialog.svelte";

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
					(unmatched === 1
						? i18n.reidentify_unmatched_one
						: i18n.reidentify_unmatched_other)({ title: res.title, count: unmatched }),
				);
			} else {
				toast.ok(
					res.renamed > 0
						? (res.renamed === 1
								? i18n.reidentify_renamed_one
								: i18n.reidentify_renamed_other)({ title: res.title, count: res.renamed })
						: i18n.reidentify_done({ title: res.title }),
				);
			}
			picked = null;
			onClose();
		},
		onError: (e) => toast.err(errorText(e, i18n.reidentify_failed())),
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
			? i18n.reidentify_body_movie({ title: currentTitle })
			: i18n.reidentify_body_series({ title: currentTitle }),
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
	title={i18n.reidentify_title({ label: pickedLabel })}
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
