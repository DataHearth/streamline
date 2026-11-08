<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Gauge, Plus } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { createOutcome } from "@lib/outcome.svelte";
	import type {
		AddMovieRequest,
		LookupDetail,
		Movie,
		QualityProfile,
		TMDBMovieResult,
	} from "@lib/types";
	import Modal from "@components/modals/Modal.svelte";
	import Select from "@components/forms/Select.svelte";
	import LookupDetailPanel from "@components/shared/LookupDetailPanel.svelte";
	import OutcomeButton from "@components/shared/OutcomeButton.svelte";
	import { serverDefaultLabel } from "@lib/quality-profiles";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		rec: TMDBMovieResult | null;
		onClose: () => void;
	};
	let { open, rec, onClose }: Props = $props();

	let qualityProfileName = $state<string>("");

	// The add's result, shown on the add button itself. On success the modal
	// closes after a second, while the button is still green; the outcome is
	// reset as it opens, so the closing modal never shows the idle button.
	const outcome = createOutcome<number>();
	let closeTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		if (open) outcome.clear();
		else {
			qualityProfileName = "";
			clearTimeout(closeTimer);
		}
	});
	$effect(() => () => clearTimeout(closeTimer));

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: open,
	}));

	let qpOptions = $derived([
		{
			value: "",
			label: auth.canAddDirectly
				? serverDefaultLabel(qpQuery.data, "movie")
				: i18n.quality_no_preference(),
		},
		...(qpQuery.data ?? []).map((p) => ({
			value: p.name,
			label: p.name,
		})),
	]);

	// Same lazy fetch the add modal makes: the recommendation carries a title
	// and a truncated overview, and the decision to add wants the rest.
	const detailQuery = createQuery<LookupDetail>(() => ({
		queryKey: ["tmdb-detail", rec?.tmdb_id ?? null],
		queryFn: () => api<LookupDetail>(`/search/movie/${rec?.tmdb_id}`),
		enabled: open && !!rec,
		staleTime: 5 * 60_000,
	}));

	let panelItem = $derived(
		rec
			? {
					title: rec.title,
					year: rec.year,
					poster_url: rec.poster_url,
					overview: rec.overview,
					subtitle:
						rec.original_title.trim() &&
						rec.original_title.trim() !== rec.title.trim()
							? rec.original_title
							: undefined,
				}
			: undefined,
	);

	const qc = useQueryClient();
	const addMutation = createMutation<Movie | null, Error, TMDBMovieResult>(() => ({
		onMutate: () => outcome.clear(),
		mutationFn: async (m) => {
			if (!auth.canAddDirectly) {
				await api("/requests", {
					method: "POST",
					body: {
						media_type: "movie",
						media_id: m.tmdb_id,
						title: m.title,
						quality_profile: qualityProfileName || undefined,
					},
				});
				return null;
			}
			const body: AddMovieRequest = { tmdb_id: m.tmdb_id };
			if (qualityProfileName !== "") {
				body.quality_profile = qualityProfileName;
			}
			return api<Movie>("/movies", { method: "POST", body });
		},
		onSuccess: (movie, m) => {
			outcome.done(m.tmdb_id);
			if (movie) {
				qc.invalidateQueries({ queryKey: ["movies"] });
				qc.invalidateQueries({ queryKey: ["movies", "counts"] });
			} else {
				qc.invalidateQueries({ queryKey: ["requests"] });
			}
			closeTimer = setTimeout(onClose, 1000);
		},
		onError: (e, m) =>
			outcome.failed(
				m.tmdb_id,
				errorText(e, auth.canAddDirectly ? i18n.common_add_failed() : i18n.common_request_failed()),
			),
	}));
</script>

<Modal {open} title={auth.canAddDirectly ? i18n.action_add_to_library() : i18n.action_request()} size="2xl" {onClose}>
	{#snippet children()}
		{#if rec}
			<LookupDetailPanel
				kind="movie"
				item={panelItem}
				detail={detailQuery.data}
				loading={detailQuery.isLoading}
				error={detailQuery.isError
					? errorText(detailQuery.error, i18n.torrent_details_failed())
					: undefined}
				compact
			/>
		{/if}
	{/snippet}

	{#snippet footer()}
		<div class="mr-auto flex items-center gap-2">
			<label
				for="add-rec-qp"
				class="inline-flex shrink-0 items-center gap-1.5 text-sm font-medium text-fg"
			>
				<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
				{i18n.quality_profile()}
			</label>
			<Select
				id="add-rec-qp"
				value={qualityProfileName}
				options={qpOptions}
				onChange={(v) => (qualityProfileName = v)}
			/>
		</div>
		<button
			type="button"
			onclick={onClose}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			{i18n.common_cancel()}
		</button>
		{#if rec}
			<OutcomeButton
				class="flex-1 sm:flex-none"
				phase={outcome.phase(rec.tmdb_id, addMutation.isPending)}
				error={outcome.error(rec.tmdb_id)}
				icon={Plus}
				label={auth.canAddDirectly ? i18n.action_add_to_library() : i18n.action_request()}
				pendingLabel={auth.canAddDirectly ? i18n.action_adding() : i18n.action_requesting()}
				doneLabel={auth.canAddDirectly ? i18n.common_added() : i18n.status_requested()}
				failedLabel={auth.canAddDirectly ? i18n.common_add_failed() : i18n.common_request_failed()}
				onclick={() => rec && addMutation.mutate(rec)}
			/>
		{/if}
	{/snippet}
</Modal>
