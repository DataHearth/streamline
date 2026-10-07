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
	import { hardcoverUnavailable } from "@lib/books";
	import { toast } from "@lib/toast";
	import { cn } from "@lib/cn";
	import { initials } from "@lib/people";
	import { INPUT_CLASS } from "@lib/form";
	import { bookAuthorAdd } from "@lib/schemas";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import type {
		AudiobookQualityProfile,
		BookAuthor,
		BookAuthorSearchResult,
		BookMonitorPolicy,
		BookWantKinds,
		EbookQualityProfile,
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

	const search = createQuery<{ items: BookAuthorSearchResult[] }>(() => ({
		queryKey: ["books", "search", debounced],
		queryFn: () =>
			api<{ items: BookAuthorSearchResult[] }>(
				`/books/search?query=${encodeURIComponent(debounced)}`,
			),
		enabled: debounced !== "",
		placeholderData: keepPreviousData,
		retry: false,
	}));

	const library = createQuery<Paginated<BookAuthor>>(() => ({
		queryKey: ["books", "authors"],
		queryFn: () => apiAllPages<BookAuthor>("/books/authors"),
	}));
	let idByHardcoverId = $derived(
		new Map((library.data?.items ?? []).map((a) => [a.hardcover_id, a.id])),
	);

	const ebookProfiles = createQuery<EbookQualityProfile[]>(() => ({
		queryKey: ["books", "ebook-quality-profiles"],
		queryFn: () => api<EbookQualityProfile[]>("/books/ebook-quality-profiles"),
	}));
	const audiobookProfiles = createQuery<AudiobookQualityProfile[]>(() => ({
		queryKey: ["books", "audiobook-quality-profiles"],
		queryFn: () =>
			api<AudiobookQualityProfile[]>("/books/audiobook-quality-profiles"),
	}));
	let ebookOptions = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...(ebookProfiles.data ?? []).map((p) => ({
			value: p.name,
			label: p.name,
		})),
	]);
	let audiobookOptions = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...(audiobookProfiles.data ?? []).map((p) => ({
			value: p.name,
			label: p.name,
		})),
	]);

	const policyOptions: { value: BookMonitorPolicy; label: string }[] = [
		{ value: "all", label: i18n.books_policy_all() },
		{ value: "future", label: i18n.books_policy_future() },
		{ value: "none", label: i18n.books_policy_none() },
	];
	const kindsOptions: { value: BookWantKinds; label: string }[] = [
		{ value: "ebook", label: i18n.books_kinds_ebook() },
		{ value: "audiobook", label: i18n.books_kinds_audiobook() },
		{ value: "both", label: i18n.books_kinds_both() },
	];

	let picked = $state<BookAuthorSearchResult | null>(null);
	let addUnavailable = $state(false);

	type AddValues = {
		monitored: boolean;
		monitor_policy: BookMonitorPolicy;
		want_kinds: BookWantKinds;
		ebook_quality_profile: string;
		audiobook_quality_profile: string;
	};
	const DEFAULTS: AddValues = {
		monitored: true,
		monitor_policy: "all",
		want_kinds: "both",
		ebook_quality_profile: "",
		audiobook_quality_profile: "",
	};

	const add = createMutation<BookAuthor, Error, AddValues>(() => ({
		mutationFn: (values) =>
			api<BookAuthor>("/books/authors", {
				method: "POST",
				body: {
					hardcover_id: picked?.hardcover_id,
					monitored: values.monitored,
					monitor_policy: values.monitor_policy,
					want_kinds: values.want_kinds,
					...(values.ebook_quality_profile
						? { ebook_quality_profile: values.ebook_quality_profile }
						: {}),
					...(values.audiobook_quality_profile
						? { audiobook_quality_profile: values.audiobook_quality_profile }
						: {}),
				},
			}),
		onMutate: () => (addUnavailable = false),
		onSuccess: (author) => {
			qc.invalidateQueries({ queryKey: ["books"] });
			toast.ok(i18n.books_added({ name: author.name }));
			navigate(`/books/author/${author.id}`);
		},
		onError: (err) => {
			if (hardcoverUnavailable(err)) {
				addUnavailable = true;
				return;
			}
			toast.err(errorText(err, i18n.common_add_failed()));
		},
	}));

	const form = createForm(() => ({
		defaultValues: DEFAULTS,
		validators: { onChange: bookAuthorAdd },
		onSubmit: ({ value }) => add.mutate(value),
	}));

	function pick(r: BookAuthorSearchResult) {
		picked = r;
		addUnavailable = false;
		form.reset(DEFAULTS);
	}

	let items = $derived(search.data?.items ?? []);
</script>

{#snippet hardcoverNotConfigured()}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center"
		role="status"
	>
		<p class="text-sm text-fg">{i18n.books_hardcover_unconfigured()}</p>
		<p class="mt-1 text-xs text-fg-muted">
			{i18n.books_hardcover_unconfigured_help()}
		</p>
		<a
			href="/settings/metadata"
			class="mt-3 inline-flex rounded-md border border-border px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
		>
			{i18n.books_hardcover_settings_link()}
		</a>
	</div>
{/snippet}

<div class="mx-auto flex max-w-3xl flex-col gap-5 px-4 py-4 md:px-6">
	<a
		href="/books"
		class="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
	>
		<ArrowLeft size={13} aria-hidden="true" />
		{i18n.books_label()}
	</a>

	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.books_add_author()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">{i18n.books_add_intro()}</p>
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
				placeholder={i18n.books_search_placeholder()}
				aria-label={i18n.books_search_placeholder()}
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
					<p class="min-w-0 truncate text-sm font-semibold text-fg">
						{picked.name}
					</p>
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
							description={i18n.books_add_monitored_help()}
						/>
					{/snippet}
				</form.Field>

				<div class="grid gap-4 sm:grid-cols-2">
					<form.Field name="monitor_policy">
						{#snippet children(field)}
							<Select
								label={i18n.books_monitor_policy_label()}
								value={field.state.value}
								options={policyOptions}
								onChange={(v) => field.handleChange(v)}
							/>
						{/snippet}
					</form.Field>

					<form.Field name="want_kinds">
						{#snippet children(field)}
							<Select
								label={i18n.books_kinds_label()}
								value={field.state.value}
								options={kindsOptions}
								onChange={(v) => field.handleChange(v)}
							/>
						{/snippet}
					</form.Field>

					<form.Field name="ebook_quality_profile">
						{#snippet children(field)}
							<Select
								label={i18n.books_ebook_profile_label()}
								value={field.state.value}
								options={ebookOptions}
								onChange={(v) => field.handleChange(v)}
							/>
						{/snippet}
					</form.Field>

					<form.Field name="audiobook_quality_profile">
						{#snippet children(field)}
							<Select
								label={i18n.books_audiobook_profile_label()}
								value={field.state.value}
								options={audiobookOptions}
								onChange={(v) => field.handleChange(v)}
							/>
						{/snippet}
					</form.Field>
				</div>

				{#if addUnavailable}
					{@render hardcoverNotConfigured()}
				{/if}

				<div class="flex items-center gap-3">
					<button
						type="submit"
						disabled={add.isPending || !form.state.canSubmit}
						class="inline-flex items-center rounded-md bg-accent px-4 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
					>
						{add.isPending ? i18n.action_adding() : i18n.books_add_author()}
					</button>
					{#if add.isPending}
						<p class="text-xs text-fg-muted" role="status">
							{i18n.books_add_slow()}
						</p>
					{/if}
				</div>
			</form>
		{/if}

		{#if debounced === ""}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.books_search_prompt()}
			</p>
		{:else if search.isPending}
			<div class="space-y-3">
				<SkeletonList variant="row" count={4} />
			</div>
		{:else if search.isError}
			{#if hardcoverUnavailable(search.error)}
				{@render hardcoverNotConfigured()}
			{:else}
				<p class="text-sm text-status-failed">
					{i18n.err_load_failed_detail({ reason: errorText(search.error) })}
				</p>
			{/if}
		{:else if items.length === 0}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.common_no_matches()}
			</p>
		{:else}
			<ul class="space-y-2">
				{#each items as r (r.hardcover_id)}
					{@const libraryId = idByHardcoverId.get(r.hardcover_id)}
					<li
						class="flex items-center gap-3 rounded-lg border border-border bg-bg-elevated p-3"
					>
						<span
							class="relative grid h-10 w-10 shrink-0 place-items-center overflow-hidden rounded-full bg-bg-card text-sm font-semibold text-fg-muted"
						>
							<span aria-hidden="true">{initials(r.name)}</span>
						</span>
						<div class="min-w-0 flex-1">
							<p class="truncate text-sm font-semibold text-fg">{r.name}</p>
							<p class="truncate text-xs text-fg-muted">
								{r.books_count === 1
									? i18n.books_count_one({ count: 1 })
									: i18n.books_count_other({ count: r.books_count })}
							</p>
						</div>
						{#if r.already_added}
							<a
								href={libraryId ? `/books/author/${libraryId}` : "/books"}
								class="shrink-0 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
							>
								{i18n.books_in_library()}
							</a>
						{:else}
							<button
								type="button"
								onclick={() => pick(r)}
								disabled={add.isPending}
								class="shrink-0 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
							>
								{i18n.books_select()}
							</button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</div>
