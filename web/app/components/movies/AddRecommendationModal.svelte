<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Gauge, LoaderCircle, Plus } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
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
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		rec: TMDBMovieResult | null;
		onClose: () => void;
	};
	let { open, rec, onClose }: Props = $props();

	let qualityProfileName = $state<string>("");

	$effect(() => {
		if (!open) qualityProfileName = "";
	});

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: open,
	}));

	let qpOptions = $derived([
		{
			value: "",
			label: auth.canAddDirectly
				? i18n.quality_server_default()
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
			if (movie) {
				qc.invalidateQueries({ queryKey: ["movies"] });
				qc.invalidateQueries({ queryKey: ["movies", "counts"] });
				toast.ok(i18n.toast_added({ title: m.title }));
			} else {
				qc.invalidateQueries({ queryKey: ["requests"] });
				toast.ok(i18n.toast_requested({ title: m.title }));
			}
			onClose();
		},
		onError: (e) => toast.err(errorText(e, i18n.common_add_failed())),
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
		<button
			type="button"
			disabled={!rec || addMutation.isPending}
			onclick={() => rec && addMutation.mutate(rec)}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-2 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{#if addMutation.isPending}
				<LoaderCircle size={14} class="animate-spin" aria-hidden="true" />
				{auth.canAddDirectly ? "Adding…" : i18n.action_requesting()}
			{:else}
				<Plus size={14} aria-hidden="true" />
				{auth.canAddDirectly ? "Add to library" : i18n.action_request()}
			{/if}
		</button>
	{/snippet}
</Modal>
