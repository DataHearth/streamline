<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { BookOpen, Bookmark } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { cn } from "@lib/cn";
	import { bookPosterUrl } from "@lib/posters";
	import { bookSlotStatus } from "@lib/status";
	import { toast } from "@lib/toast";
	import Poster from "@components/shared/Poster.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import type { Book, BookEntry } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { book }: { book: BookEntry } = $props();

	const qc = useQueryClient();

	type Slot = "ebook" | "audiobook";

	const monitor = createMutation<
		Book,
		Error,
		{ slot: Slot; monitored: boolean }
	>(() => ({
		mutationFn: ({ slot, monitored }) =>
			api<Book>(`/books/${book.id}`, {
				method: "PATCH",
				body:
					slot === "ebook"
						? { ebook_monitored: monitored }
						: { audiobook_monitored: monitored },
			}),
		onSuccess: () => qc.invalidateQueries({ queryKey: ["books"] }),
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	let year = $derived(book.release_date?.slice(0, 4) ?? "");
	let series = $derived(
		book.series_name
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

<li class="rounded-lg border border-border bg-bg-elevated">
	<div class="flex flex-wrap items-center gap-3 p-3">
		<a
			href="/books/{book.id}"
			class="flex min-w-0 flex-1 basis-56 items-center gap-3"
		>
			<span
				class="relative grid h-12 w-9 shrink-0 place-items-center overflow-hidden rounded bg-bg-card text-fg-faint"
			>
				<BookOpen size={18} aria-hidden="true" />
				<Poster
					src={bookPosterUrl(book.id)}
					alt={i18n.books_cover_alt({ title: book.title })}
					class="absolute inset-0 h-full w-full object-cover"
				/>
			</span>
			<span class="min-w-0 flex-1">
				<span class="block truncate text-sm font-semibold text-fg">
					{book.title}
				</span>
				<span class="mt-0.5 block truncate text-xs text-fg-muted">
					{[series, year].filter(Boolean).join(" · ")}
				</span>
			</span>
		</a>
		<div class="flex flex-wrap items-center gap-3">
			{#each slots as s (s.key)}
				{@const slot = book[s.key]}
				<div class="flex items-center gap-1.5">
					<span class="text-[11px] font-medium text-fg-subtle">{s.label}</span>
					<StatusPill status={bookSlotStatus(slot)} size="sm" />
					{#if auth.canAddDirectly}
						<button
							type="button"
							onclick={() =>
								monitor.mutate({ slot: s.key, monitored: !slot.monitored })}
							disabled={monitor.isPending}
							aria-pressed={slot.monitored}
							aria-label={s.key === "ebook"
								? slot.monitored
									? i18n.books_unmonitor_ebook()
									: i18n.books_monitor_ebook()
								: slot.monitored
									? i18n.books_unmonitor_audiobook()
									: i18n.books_monitor_audiobook()}
							title={slot.monitored
								? i18n.action_stop_monitoring()
								: i18n.action_monitor()}
							class={cn(
								"grid h-8 w-8 shrink-0 place-items-center rounded-md border transition disabled:opacity-60",
								slot.monitored
									? "border-accent-line bg-accent-soft text-accent-text"
									: "border-border-strong text-fg-muted hover:text-fg",
							)}
						>
							<Bookmark
								size={14}
								fill={slot.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
						</button>
					{/if}
				</div>
			{/each}
		</div>
	</div>
</li>
