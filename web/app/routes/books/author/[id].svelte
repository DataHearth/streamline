<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { goto, params } from "@roxi/routify";
	import { onMount } from "svelte";
	import { ArrowLeft, Bookmark, RefreshCw, Trash2 } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { hardcoverUnavailable } from "@lib/books";
	import { cn } from "@lib/cn";
	import { initials } from "@lib/people";
	import { authorPosterUrl } from "@lib/posters";
	import { toast } from "@lib/toast";
	import BookRow from "@components/books/BookRow.svelte";
	import Select from "@components/forms/Select.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import Poster from "@components/shared/Poster.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import type {
		AudiobookQualityProfile,
		BookAuthor,
		BookMonitorPolicy,
		BookWantKinds,
		EbookQualityProfile,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let routeParams = $state<Record<string, string>>({});
	let navigate = $state<(path: string) => void>(() => {});
	let deleteOpen = $state(false);

	onMount(() => {
		// Routify reuses this instance across /books/author/[id] param changes,
		// so the per-author UI state is cleared by hand. The first emission only
		// records the id.
		let currentId: string | undefined;
		const u1 = params.subscribe((p) => {
			if (currentId !== undefined && p.id !== currentId) {
				deleteOpen = false;
			}
			currentId = p.id;
			routeParams = p;
		});
		const u2 = goto.subscribe((fn) => (navigate = fn));
		return () => {
			u1();
			u2();
		};
	});
	const authorId = $derived(Number(routeParams.id));

	const qc = useQueryClient();

	const authorQuery = createQuery<BookAuthor>(() => ({
		queryKey: ["books", "author", authorId],
		queryFn: () => api<BookAuthor>(`/books/authors/${authorId}`),
		enabled: Number.isFinite(authorId) && authorId > 0,
	}));
	let author = $derived(authorQuery.data);

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

	let books = $derived(
		(author?.books ?? []).toSorted((a, b) =>
			(a.release_date ?? "9999").localeCompare(b.release_date ?? "9999"),
		),
	);

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["books"] });
	}

	const patch = createMutation<
		BookAuthor,
		Error,
		{
			monitored?: boolean;
			monitor_policy?: BookMonitorPolicy;
			want_kinds?: BookWantKinds;
			ebook_quality_profile?: string;
			audiobook_quality_profile?: string;
		}
	>(() => ({
		mutationFn: (body) =>
			api<BookAuthor>(`/books/authors/${authorId}`, {
				method: "PATCH",
				body,
			}),
		onSuccess: () => invalidate(),
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	const refresh = createMutation<BookAuthor, Error>(() => ({
		mutationFn: () =>
			api<BookAuthor>(`/books/authors/${authorId}/refresh`, {
				method: "POST",
			}),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.books_refreshed());
		},
		onError: (e) =>
			toast.err(
				hardcoverUnavailable(e)
					? i18n.books_hardcover_unconfigured()
					: errorText(e, i18n.common_refresh_failed()),
			),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/books/authors/${authorId}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.books_deleted());
			navigate("/books");
		},
		onError: (e) => toast.err(errorText(e, i18n.common_delete_failed())),
	}));
</script>

<div class="flex flex-col gap-6 px-4 py-4 md:px-8">
	<a
		href="/books"
		class="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
	>
		<ArrowLeft size={13} aria-hidden="true" />
		{i18n.books_label()}
	</a>

	{#if authorQuery.isPending}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div class="flex items-center gap-5">
			<Skeleton w="96px" h={96} round="full" />
			<div class="flex flex-1 flex-col gap-2">
				<Skeleton w="40%" h={28} />
				<Skeleton w="70%" h={12} />
			</div>
		</div>
		<Skeleton h={64} round="md" />
		<Skeleton h={64} round="md" />
	{:else if authorQuery.isError}
		<div
			class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				{i18n.books_author_load_failed()}
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{errorText(authorQuery.error, i18n.common_unknown_error())}
			</p>
		</div>
	{:else if author}
		<header class="flex flex-col gap-5 md:flex-row md:items-start">
			<span
				class="relative grid h-24 w-24 shrink-0 place-items-center overflow-hidden rounded-full bg-bg-card text-3xl font-semibold text-fg-muted"
			>
				<span aria-hidden="true">{initials(author.name)}</span>
				<Poster
					src={authorPosterUrl(author.id)}
					alt={author.name}
					class="absolute inset-0 h-full w-full object-cover"
				/>
			</span>
			<div class="min-w-0 flex-1">
				<h1
					class="text-2xl font-bold leading-tight tracking-tight text-fg md:text-4xl"
				>
					{author.name}
				</h1>
				{#if author.overview}
					<p
						class="mt-3 line-clamp-4 max-w-3xl text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
					>
						{author.overview}
					</p>
				{/if}

				{#if auth.canAddDirectly}
					<div class="mt-4 flex flex-wrap items-center gap-2.5">
						<button
							type="button"
							onclick={() => patch.mutate({ monitored: !author.monitored })}
							disabled={patch.isPending}
							aria-pressed={author.monitored}
							class={cn(
								"inline-flex h-10 items-center gap-2 rounded-md border px-3.5 text-sm font-medium transition disabled:opacity-60",
								author.monitored
									? "border-accent-line bg-accent-soft text-accent-text"
									: "border-border-strong text-fg hover:bg-white/[0.06]",
							)}
						>
							<Bookmark
								size={15}
								fill={author.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
							{author.monitored
								? i18n.monitor_monitored()
								: i18n.action_monitor()}
						</button>

						<button
							type="button"
							onclick={() => refresh.mutate()}
							disabled={refresh.isPending}
							class="inline-flex h-10 items-center gap-2 rounded-md border border-border-strong px-3.5 text-sm font-medium text-fg transition hover:bg-white/[0.06] disabled:opacity-60"
						>
							<RefreshCw
								size={15}
								class={cn(refresh.isPending && "animate-spin")}
								aria-hidden="true"
							/>
							{refresh.isPending
								? i18n.common_refreshing()
								: i18n.action_refresh_metadata()}
						</button>

						<button
							type="button"
							onclick={() => (deleteOpen = true)}
							class="inline-flex h-10 items-center gap-2 rounded-md border border-border-strong px-3.5 text-sm font-medium text-status-failed transition hover:bg-status-failed/10"
						>
							<Trash2 size={15} aria-hidden="true" />
							{i18n.common_delete()}
						</button>
					</div>

					<div class="mt-4 grid max-w-3xl gap-3 sm:grid-cols-2">
						<Select
							label={i18n.books_monitor_policy_label()}
							value={author.monitor_policy}
							options={policyOptions}
							disabled={patch.isPending}
							onChange={(v) => patch.mutate({ monitor_policy: v })}
						/>
						<Select
							label={i18n.books_kinds_label()}
							value={author.want_kinds}
							options={kindsOptions}
							disabled={patch.isPending}
							onChange={(v) => patch.mutate({ want_kinds: v })}
						/>
						<Select
							label={i18n.books_ebook_profile_label()}
							value={author.ebook_quality_profile}
							options={ebookOptions}
							disabled={patch.isPending}
							onChange={(v) => patch.mutate({ ebook_quality_profile: v })}
						/>
						<Select
							label={i18n.books_audiobook_profile_label()}
							value={author.audiobook_quality_profile}
							options={audiobookOptions}
							disabled={patch.isPending}
							onChange={(v) => patch.mutate({ audiobook_quality_profile: v })}
						/>
					</div>
				{/if}
			</div>
		</header>

		{#if books.length === 0}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.books_no_books()}
			</p>
		{:else}
			<ul class="flex flex-col gap-2">
				{#each books as book (book.id)}
					<BookRow {book} />
				{/each}
			</ul>
		{/if}

		<DeleteTitleDialog
			open={deleteOpen}
			title={i18n.books_remove_title({ name: author.name })}
			body={i18n.books_remove_body()}
			filesLabel={i18n.books_delete_files_label()}
			filesNote={i18n.common_cannot_undo()}
			pending={del.isPending}
			onClose={() => (deleteOpen = false)}
			onConfirm={(withFiles) => del.mutate(withFiles)}
		/>
	{/if}
</div>
