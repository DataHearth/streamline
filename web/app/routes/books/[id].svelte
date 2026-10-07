<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { params } from "@roxi/routify";
	import { onMount } from "svelte";
	import { ArrowLeft, BookOpen, Bookmark } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { cn } from "@lib/cn";
	import { bookPosterUrl } from "@lib/posters";
	import { bookSlotStatus } from "@lib/status";
	import { toast } from "@lib/toast";
	import Poster from "@components/shared/Poster.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import type { Book } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let routeParams = $state<Record<string, string>>({});
	onMount(() => params.subscribe((p) => (routeParams = p)));
	const bookId = $derived(Number(routeParams.id));

	const qc = useQueryClient();

	const bookQuery = createQuery<Book>(() => ({
		queryKey: ["books", "book", bookId],
		queryFn: () => api<Book>(`/books/${bookId}`),
		enabled: Number.isFinite(bookId) && bookId > 0,
	}));
	let book = $derived(bookQuery.data);

	type Slot = "ebook" | "audiobook";

	const monitor = createMutation<
		Book,
		Error,
		{ slot: Slot; monitored: boolean }
	>(() => ({
		mutationFn: ({ slot, monitored }) =>
			api<Book>(`/books/${bookId}`, {
				method: "PATCH",
				body:
					slot === "ebook"
						? { ebook_monitored: monitored }
						: { audiobook_monitored: monitored },
			}),
		onSuccess: () => qc.invalidateQueries({ queryKey: ["books"] }),
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	let year = $derived(book?.release_date?.slice(0, 4) ?? "");
	let series = $derived(
		book?.series_name
			? book.series_position
				? i18n.books_series_position({
						series: book.series_name,
						position: book.series_position,
					})
				: book.series_name
			: "",
	);
	let slots = $derived<{ key: Slot; label: string }[]>([
		{ key: "ebook", label: i18n.books_slot_ebook() },
		{ key: "audiobook", label: i18n.books_slot_audiobook() },
	]);
</script>

<div class="flex flex-col gap-6 px-4 py-4 md:px-8">
	<a
		href={book ? `/books/author/${book.author_id}` : "/books"}
		class="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
	>
		<ArrowLeft size={13} aria-hidden="true" />
		{book ? i18n.books_back_to_author() : i18n.books_label()}
	</a>

	{#if bookQuery.isPending}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div class="flex items-start gap-5">
			<Skeleton w="128px" h={192} round="md" />
			<div class="flex flex-1 flex-col gap-2">
				<Skeleton w="50%" h={28} />
				<Skeleton w="30%" h={12} />
			</div>
		</div>
	{:else if bookQuery.isError}
		<div
			class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				{i18n.books_load_failed()}
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{errorText(bookQuery.error, i18n.common_unknown_error())}
			</p>
		</div>
	{:else if book}
		<header class="flex flex-col gap-5 md:flex-row md:items-start">
			<span
				class="relative grid h-48 w-32 shrink-0 place-items-center overflow-hidden rounded-md bg-bg-card text-fg-faint"
			>
				<BookOpen size={32} aria-hidden="true" />
				<Poster
					src={bookPosterUrl(book.id)}
					alt={i18n.books_cover_alt({ title: book.title })}
					class="absolute inset-0 h-full w-full object-cover"
				/>
			</span>
			<div class="min-w-0 flex-1">
				<h1
					class="text-2xl font-bold leading-tight tracking-tight text-fg md:text-4xl"
				>
					{book.title}
				</h1>
				<p class="mt-1 text-sm text-fg-muted">
					{[series, year].filter(Boolean).join(" · ")}
				</p>
				{#if book.overview}
					<p
						class="mt-3 max-w-3xl text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
					>
						{book.overview}
					</p>
				{/if}
			</div>
		</header>

		<div class="grid gap-3 sm:grid-cols-2">
			{#each slots as s (s.key)}
				{@const slot = book[s.key]}
				<section
					class="flex flex-col gap-3 rounded-lg border border-border bg-bg-elevated p-4"
					aria-label={s.label}
				>
					<div class="flex items-center justify-between gap-3">
						<h2 class="text-sm font-semibold text-fg">{s.label}</h2>
						<StatusPill status={bookSlotStatus(slot)} size="sm" />
					</div>
					<p class="text-xs text-fg-muted">
						{slot.file_count === 1
							? i18n.books_files_count_one({ count: 1 })
							: i18n.books_files_count_other({ count: slot.file_count })}
					</p>
					{#if auth.canAddDirectly}
						<button
							type="button"
							onclick={() =>
								monitor.mutate({ slot: s.key, monitored: !slot.monitored })}
							disabled={monitor.isPending}
							aria-pressed={slot.monitored}
							class={cn(
								"inline-flex h-9 w-fit items-center gap-2 rounded-md border px-3 text-sm font-medium transition disabled:opacity-60",
								slot.monitored
									? "border-accent-line bg-accent-soft text-accent-text"
									: "border-border-strong text-fg hover:bg-white/[0.06]",
							)}
						>
							<Bookmark
								size={14}
								fill={slot.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
							{slot.monitored ? i18n.monitor_monitored() : i18n.action_monitor()}
						</button>
					{/if}
				</section>
			{/each}
		</div>
	{/if}
</div>
