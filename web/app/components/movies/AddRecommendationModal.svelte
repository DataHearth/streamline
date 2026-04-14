<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Film, Loader2, Plus } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type {
		AddMovieRequest,
		Movie,
		QualityProfile,
		TMDBMovieResult,
	} from "../../lib/types";
	import Modal from "../modals/Modal.svelte";
	import Select from "../forms/Select.svelte";
	import Poster from "./Poster.svelte";

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
		{ value: "", label: "Server default" },
		...(qpQuery.data ?? []).map((p) => ({
			value: p.name,
			label: p.name,
		})),
	]);

	const qc = useQueryClient();
	const addMutation = createMutation<Movie, Error, TMDBMovieResult>(() => ({
		mutationFn: (m) => {
			const body: AddMovieRequest = { tmdb_id: m.tmdb_id };
			if (qualityProfileName !== "") {
				body.quality_profile = qualityProfileName;
			}
			return api<Movie>("/movies", { method: "POST", body });
		},
		onSuccess: (_movie, m) => {
			qc.invalidateQueries({ queryKey: ["movies"] });
			qc.invalidateQueries({ queryKey: ["movies", "counts"] });
			toast.ok(`Added ${m.title}`);
			onClose();
		},
		onError: (e) => toast.err(e.message ?? "Add failed"),
	}));
</script>

<Modal {open} title="Add to library" size="md" {onClose}>
	{#snippet children()}
		{#if rec}
			<div class="flex gap-4">
				<div
					class="relative aspect-[2/3] w-24 shrink-0 overflow-hidden rounded-md bg-bg-card ring-1 ring-border"
				>
					<div
						class="absolute inset-0 grid place-items-center text-fg-faint"
					>
						<Film class="h-7 w-7" aria-hidden="true" />
					</div>
					{#if rec.poster_url}
						<Poster
							src={rec.poster_url}
							alt="{rec.title} poster"
							class="relative h-full w-full object-cover"
						/>
					{/if}
				</div>
				<div class="min-w-0 flex-1">
					<h3 class="text-base font-semibold text-fg">
						{rec.title}
					</h3>
					{#if rec.original_title.trim() && rec.original_title.trim() !== rec.title.trim()}
						<p class="mt-0.5 truncate text-xs italic text-fg-faint">
							{rec.original_title}
						</p>
					{/if}
					{#if rec.year}
						<p class="mt-0.5 font-mono text-xs text-fg-faint">
							{rec.year}
						</p>
					{/if}
					{#if rec.overview}
						<p class="mt-2 line-clamp-4 text-sm text-fg-muted">
							{rec.overview}
						</p>
					{/if}
				</div>
			</div>

			<div class="mt-5">
				<Select
					label="Quality profile"
					value={qualityProfileName}
					options={qpOptions}
					onChange={(v) => (qualityProfileName = v)}
				/>
			</div>
		{/if}
	{/snippet}

	{#snippet footer()}
		<button
			type="button"
			onclick={onClose}
			class="inline-flex h-9 items-center rounded-md px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			Cancel
		</button>
		<button
			type="button"
			disabled={!rec || addMutation.isPending}
			onclick={() => rec && addMutation.mutate(rec)}
			class="inline-flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{#if addMutation.isPending}
				<Loader2 size={14} class="animate-spin" aria-hidden="true" />
				Adding…
			{:else}
				<Plus size={14} aria-hidden="true" />
				Add to library
			{/if}
		</button>
	{/snippet}
</Modal>
