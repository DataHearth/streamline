<script lang="ts">
	import {
		createMutation,
		createQuery,
		keepPreviousData,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { goto } from "@roxi/routify";
	import { onMount } from "svelte";
	import { ArrowLeft, Search, X } from "@lucide/svelte";
	import { api, apiAllPages, errorText } from "@lib/api";
	import type { Paginated } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { cn } from "@lib/cn";
	import { INPUT_CLASS } from "@lib/form";
	import { musicArtistAdd } from "@lib/schemas";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import type {
		MusicArtist,
		MusicArtistSearchResult,
		MusicQualityProfile,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const DEBOUNCE_MS = 300;

	let navigate = $state<(path: string) => void>(() => {});
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	const qc = useQueryClient();

	let input = $state("");
	let debounced = $state("");
	$effect(() => {
		const next = input.trim();
		const t = setTimeout(() => (debounced = next), DEBOUNCE_MS);
		return () => clearTimeout(t);
	});

	const search = createQuery<{ items: MusicArtistSearchResult[] }>(() => ({
		queryKey: ["music", "search", debounced],
		queryFn: () =>
			api<{ items: MusicArtistSearchResult[] }>(
				`/music/search?query=${encodeURIComponent(debounced)}`,
			),
		enabled: debounced !== "",
		placeholderData: keepPreviousData,
		retry: false,
	}));

	const library = createQuery<Paginated<MusicArtist>>(() => ({
		queryKey: ["music", "artists"],
		queryFn: () => apiAllPages<MusicArtist>("/music/artists"),
	}));
	let idByMbid = $derived(
		new Map((library.data?.items ?? []).map((a) => [a.mbid, a.id])),
	);

	const profiles = createQuery<MusicQualityProfile[]>(() => ({
		queryKey: ["music", "quality-profiles"],
		queryFn: () => api<MusicQualityProfile[]>("/music/quality-profiles"),
	}));
	let profileOptions = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...(profiles.data ?? []).map((p) => ({ value: p.name, label: p.name })),
	]);

	let picked = $state<MusicArtistSearchResult | null>(null);

	type AddValues = { monitored: boolean; quality_profile: string };
	const add = createMutation<MusicArtist, Error, AddValues>(() => ({
		mutationFn: (values) =>
			api<MusicArtist>("/music/artists", {
				method: "POST",
				body: {
					mbid: picked?.mbid,
					monitored: values.monitored,
					...(values.quality_profile
						? { quality_profile: values.quality_profile }
						: {}),
				},
			}),
		onSuccess: (artist) => {
			qc.invalidateQueries({ queryKey: ["music", "artists"] });
			qc.invalidateQueries({ queryKey: ["music", "search"] });
			toast.ok(i18n.music_added({ name: artist.name }));
			navigate(`/music/${artist.id}`);
		},
		onError: (err) => toast.err(errorText(err, i18n.common_add_failed())),
	}));

	const form = createForm(() => ({
		defaultValues: { monitored: true, quality_profile: "" } as AddValues,
		validators: { onChange: musicArtistAdd },
		onSubmit: ({ value }) => add.mutate(value),
	}));

	function pick(r: MusicArtistSearchResult) {
		picked = r;
		form.reset({ monitored: true, quality_profile: "" });
	}

	let items = $derived(search.data?.items ?? []);
</script>

<div class="mx-auto flex max-w-3xl flex-col gap-5 px-4 py-4 md:px-6">
	<a
		href="/music"
		class="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
	>
		<ArrowLeft size={13} aria-hidden="true" />
		{i18n.music_label()}
	</a>

	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.music_add_artist()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">{i18n.music_add_intro()}</p>
	</header>

	{#if !auth.canAddDirectly}
		<p class="text-sm text-fg-muted">{i18n.err_forbidden()}</p>
	{:else}
		<div class="relative">
			<Search
				size={15}
				class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-faint"
				aria-hidden="true"
			/>
			<input
				type="search"
				bind:value={input}
				placeholder={i18n.music_search_placeholder()}
				aria-label={i18n.music_search_placeholder()}
				class={cn(INPUT_CLASS, "pl-9 pr-9")}
			/>
			{#if input}
				<button
					type="button"
					onclick={() => (input = "")}
					aria-label={i18n.common_clear_search()}
					class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-fg-muted transition hover:text-fg"
				>
					<X size={14} aria-hidden="true" />
				</button>
			{/if}
		</div>

		{#if picked}
			<form
				onsubmit={(e) => {
					e.preventDefault();
					form.handleSubmit();
				}}
				class="space-y-4 rounded-lg border border-accent-line bg-bg-elevated p-4"
			>
				<div class="flex items-start justify-between gap-3">
					<div class="min-w-0">
						<p class="truncate text-sm font-semibold text-fg">
							{picked.name}
						</p>
						{#if picked.disambiguation}
							<p class="truncate text-xs text-fg-muted">
								{picked.disambiguation}
							</p>
						{/if}
					</div>
					<button
						type="button"
						onclick={() => (picked = null)}
						disabled={add.isPending}
						aria-label={i18n.common_cancel()}
						class="rounded p-1 text-fg-muted transition hover:text-fg disabled:opacity-60"
					>
						<X size={16} aria-hidden="true" />
					</button>
				</div>

				<form.Field name="monitored">
					{#snippet children(field)}
						<Checkbox
							name={field.name}
							checked={field.state.value}
							onChange={(v) => field.handleChange(v)}
							label={i18n.monitor_monitored()}
							description={i18n.music_add_monitored_help()}
						/>
					{/snippet}
				</form.Field>

				<form.Field name="quality_profile">
					{#snippet children(field)}
						<Select
							label={i18n.quality_profile()}
							value={field.state.value}
							options={profileOptions}
							onChange={(v) => field.handleChange(v)}
						/>
					{/snippet}
				</form.Field>

				<div class="flex items-center gap-3">
					<button
						type="submit"
						disabled={add.isPending || !form.state.canSubmit}
						class="inline-flex items-center rounded-md bg-accent px-4 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
					>
						{add.isPending ? i18n.action_adding() : i18n.music_add_artist()}
					</button>
					{#if add.isPending}
						<p class="text-xs text-fg-muted" role="status">
							{i18n.music_add_slow()}
						</p>
					{/if}
				</div>
			</form>
		{/if}

		{#if debounced === ""}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.music_search_prompt()}
			</p>
		{:else if search.isPending}
			<div class="space-y-3">
				<SkeletonList variant="row" count={4} />
			</div>
		{:else if search.isError}
			<p class="text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(search.error) })}
			</p>
		{:else if items.length === 0}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.common_no_matches()}
			</p>
		{:else}
			<ul class="space-y-2">
				{#each items as r (r.mbid)}
					{@const libraryId = idByMbid.get(r.mbid)}
					<li
						class="flex items-center gap-3 rounded-lg border border-border bg-bg-elevated p-3"
					>
						<div class="min-w-0 flex-1">
							<p class="truncate text-sm font-semibold text-fg">{r.name}</p>
							{#if r.disambiguation}
								<p class="truncate text-xs text-fg-muted">
									{r.disambiguation}
								</p>
							{/if}
						</div>
						{#if r.already_added}
							<a
								href={libraryId ? `/music/${libraryId}` : "/music"}
								class="shrink-0 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
							>
								{i18n.music_in_library()}
							</a>
						{:else}
							<button
								type="button"
								onclick={() => pick(r)}
								disabled={add.isPending}
								class="shrink-0 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
							>
								{i18n.music_select()}
							</button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</div>
