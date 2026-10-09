<script lang="ts">
	import { onMount, tick } from "svelte";
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { params } from "@roxi/routify";
	import { Bookmark, LoaderCircle, Radar, Search } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import { formatRelative } from "@lib/dates";
	import Dialog from "@components/modals/Dialog.svelte";
	import Modal from "@components/modals/Modal.svelte";
	import ReleasesTable from "@components/shared/ReleasesTable.svelte";
	import MediaHero from "@components/shared/MediaHero.svelte";
	import MonitorSelect from "@components/shared/MonitorSelect.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import BookCover from "@components/books/BookCover.svelte";
	import FormatCard from "@components/books/FormatCard.svelte";
	import EditionsTable from "@components/books/EditionsTable.svelte";
	import LangChip from "@components/books/LangChip.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import PeopleGrid from "@components/shared/PeopleGrid.svelte";
	import InfoCard, { type InfoRow } from "@components/shared/InfoCard.svelte";
	import {
		bookPeople,
		bookRoleLabel,
		editionDetail,
		formatLabel,
		hasLocalEdition,
		kindLabel,
		languageName,
		personHref,
		type Book,
		type BookFormat,
		type BookMonitor,
		type Edition,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let id = $state("");
	onMount(() => params.subscribe((p) => (id = p.id ?? "")));

	const bookQuery = createQuery<Book>(() => ({
		queryKey: ["books", "book", id],
		queryFn: () => api<Book>(`/books/${id}`),
		enabled: !!id,
	}));
	let book = $derived(bookQuery.data);
	let canEdit = $derived(auth.canAddDirectly);
	let filter = $state<"all" | BookFormat>("all");
	let editionsEl = $state<HTMLElement | null>(null);
	let pending = $state<Edition | null>(null);
	let manualOpen = $state(false);
	let downloading = $derived(!!book?.formats.some((f) => f.state === "downloading"));

	const editionOf = (eid: number | null | undefined) => book?.editions.find((e) => e.id === eid);
	let slots = $derived(book ? (["ebook", "audiobook"] as BookFormat[]).map((f) => book.formats.find((s) => s.format === f)).filter((s) => !!s) : []);
	let inUse = $derived<Record<BookFormat, number | null>>({
		ebook: book?.formats.find((s) => s.format === "ebook" && s.state !== "unmonitored")?.edition_id ?? null,
		audiobook: book?.formats.find((s) => s.format === "audiobook" && s.state !== "unmonitored")?.edition_id ?? null,
	});
	let pendingSlot = $derived(pending ? book?.formats.find((s) => s.format === pending?.format) : undefined);
	let pendingCurrent = $derived(editionOf(pendingSlot?.edition_id));

	const MONITOR: { key: BookMonitor; label: string }[] = [
		{ key: "both", label: i18n.books_monitor_both() },
		{ key: "ebook", label: i18n.books_monitor_ebook() },
		{ key: "audiobook", label: i18n.books_monitor_audiobook() },
		{ key: "none", label: i18n.books_monitor_none() },
	];

	const qc = useQueryClient();
	const invalidate = () => qc.invalidateQueries({ queryKey: ["books"] });
	async function setMonitor(v: string) {
		await api(`/books/${id}`, { method: "PATCH", body: { monitor: v } });
		invalidate();
	}
	let lastMonitor: BookMonitor = "both";
	function toggleMonitor() {
		if (!book) return;
		if (book.monitor === "none") setMonitor(lastMonitor);
		else {
			lastMonitor = book.monitor;
			setMonitor("none");
		}
	}
	function monitorFormat(f: BookFormat) {
		if (!book) return;
		const other = book.formats.find((s) => s.format !== f);
		const otherOn = !!other && other.state !== "unmonitored";
		setMonitor(otherOn ? "both" : f);
	}
	async function search(f: BookFormat) {
		if (!book) return;
		await api(`/books/${id}/search`, { method: "POST", body: { format: f } });
		toast.ok(i18n.books_search_started({ title: book.title, format: formatLabel(f).toLowerCase() }));
	}
	// Every monitored format at once: the book's counterpart to a movie's
	// automatic search.
	async function searchAll() {
		if (!book) return;
		await api(`/books/${id}/search`, { method: "POST" });
		toast.ok(i18n.books_search_all_started({ title: book.title }));
	}
	async function changeEdition(f: BookFormat) {
		filter = f;
		await tick();
		const main = document.getElementById("main");
		const el = editionsEl;
		if (!main || !el) return;
		const top = el.getBoundingClientRect().top - main.getBoundingClientRect().top + main.scrollTop - 88;
		main.scrollTo({ top, behavior: "smooth" });
	}
	function use(e: Edition) {
		const slot = book?.formats.find((s) => s.format === e.format);
		if (slot?.file && slot.edition_id !== e.id) pending = e;
		else applyEdition(e);
	}
	async function applyEdition(e: Edition) {
		await api(`/books/${id}`, { method: "PATCH", body: { format: e.format, edition_id: e.id } });
		invalidate();
		toast.ok(i18n.books_edition_changed({ format: formatLabel(e.format) }));
	}

	let meta = $derived(
		book
			? [book.author, i18n.books_first_published({ year: String(book.first_published) }), (book.editions.length === 1 ? i18n.books_editions_n_one : i18n.books_editions_n_other)({ count: String(book.editions.length) })]
			: [],
	);
	const caps = "font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted";
	const h3 = "font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";

	// The movie overview's two columns, under the format cards: what the book
	// is and who made it, beside its facts and our copy of it. Translators and
	// narrators come off the editions, the ones in use first.
	let people = $derived(
		book
			? bookPeople(book.contributors ?? [], book.editions, [inUse.ebook, inUse.audiobook]).map((x) => ({
					key: `${x.role}:${x.person.name}`,
					name: x.person.name,
					photo_url: x.person.photo_url,
					href: personHref(x.person),
					role: bookRoleLabel(x.role),
					note: x.languages.map((l) => languageName(l, true)).join(", "),
				}))
			: [],
	);
	let detailRows = $derived.by<InfoRow[]>(() => {
		if (!book) return [];
		const original = book.editions.find((e) => e.original);
		const pages = (editionOf(inUse.ebook) ?? original)?.pages;
		const rows: InfoRow[] = [{ label: i18n.common_type(), value: kindLabel(book.kind), mono: false }];
		if (book.genre) rows.push({ label: i18n.music_fact_genre(), value: book.genre, mono: false });
		rows.push({ label: i18n.lookup_first_published(), value: String(book.first_published) });
		if (book.original_title) rows.push({ label: i18n.books_fact_original_title(), value: book.original_title, mono: false });
		if (original) rows.push({ label: i18n.books_fact_original_language(), value: languageName(original.language, true), mono: false });
		if (pages) rows.push({ label: i18n.lookup_pages(), value: pages.toLocaleString() });
		if (book.rating) rows.push({ label: i18n.books_fact_rating(), value: `★ ${book.rating.toFixed(1)}` });
		if (book.hardcover_id)
			rows.push({ label: "Hardcover", value: [{ text: String(book.hardcover_id), href: `https://hardcover.app/books/${book.hardcover_id}`, external: true }], mono: true });
		return rows;
	});
	let libraryRows = $derived.by<InfoRow[]>(() => {
		if (!book) return [];
		const size = book.formats.reduce((s, f) => s + (f.file?.size ?? 0), 0);
		const rows: InfoRow[] = [
			{ label: i18n.quality_profile(), value: book.quality_profile || i18n.quality_server_default() },
			{ label: i18n.action_monitor(), value: MONITOR.find((o) => o.key === book.monitor)?.label ?? "", mono: false },
			{ label: i18n.common_language(), value: languageName(book.preferred_language, true), mono: false },
		];
		if (size > 0) rows.push({ label: i18n.music_fact_on_disk(), value: formatBytes(size, "") });
		rows.push({ label: i18n.common_added(), value: formatRelative(book.added_at) });
		return rows;
	});
</script>

{#if bookQuery.isLoading || !id}
	<span class="sr-only" role="status">{i18n.common_loading()}</span>
	<div class="px-4 pt-6 md:px-8">
		<Skeleton w="80px" h={28} />
		<div class="mt-8 flex flex-col items-center gap-6 md:flex-row md:items-end">
			<div class="aspect-[2/3] w-44 animate-pulse rounded-lg bg-white/[0.06] md:w-[190px]"></div>
			<div class="flex w-full flex-col gap-3">
				<Skeleton w="60%" h={36} />
				<Skeleton w="40%" h={14} />
			</div>
		</div>
	</div>
{:else if bookQuery.isError || !book}
	<div class="px-4 pt-6 md:px-8">
		<div class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center">
			<p class="text-sm font-semibold text-status-failed">{i18n.books_load_book_failed()}</p>
			<p class="mt-1 text-xs text-fg-subtle">{errorText(bookQuery.error, i18n.common_unknown_error())}</p>
		</div>
	</div>
{:else}
	<MediaHero
		backdrop={book.cover_url}
		backHref="/books"
		backLabel={i18n.books_label()}
		cols="md:grid-cols-[190px_1fr] lg:grid-cols-[240px_1fr]"
		artWrap="w-44"
		labelId="book-title"
		title={book.title}
		originalTitle={book.original_title}
		{meta}
		overview={book.overview}
	>
		{#snippet art()}
			<div class="shadow-[0_24px_48px_rgb(0_0_0_/0.5)]">
				<BookCover src={book.cover_url} alt={i18n.common_poster_alt({ title: book.title })} />
			</div>
		{/snippet}
		{#snippet pills()}
			<StatusPill status={book.status} size="md" variant="translucent" live={book.status === "downloading"} />
			<span class={caps}>{kindLabel(book.kind)}</span>
			{#if book.genre}
				<span class="text-fg-faint" aria-hidden="true">·</span>
				<span class={caps}>{book.genre}</span>
			{/if}
		{/snippet}
		{#snippet actions()}
			{#if canEdit}
				<!-- Trial: from md, two lines — what the book follows (label, select,
				     bookmark), then the actions (both searches and the menu). One
				     wrapping flow in DOM order, broken by a full-width spacer from md,
				     so a phone keeps its old shape: label over select + bookmark +
				     menu, with the searches in the pinned bar. No "Downloading…"
				     chip: the status pill above the title already says it. -->
				<div class="flex flex-wrap items-center gap-2 md:gap-x-2.5 md:gap-y-3">
					<span class="basis-full font-mono text-[10.5px] uppercase tracking-[0.1em] text-fg-subtle md:basis-auto">{i18n.action_monitor()}</span>
					<MonitorSelect value={book.monitor} options={MONITOR} onChange={setMonitor} label={i18n.action_monitor()} class="flex-1 md:flex-none" />
					<button
						type="button"
						onclick={toggleMonitor}
						aria-pressed={book.monitor !== "none"}
						aria-label={book.monitor !== "none" ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						title={book.monitor !== "none" ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						class={cn(
							"grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated/80 transition hover:border-border-strong",
							book.monitor !== "none" ? "text-accent-text" : "text-fg-subtle",
						)}
					>
						<Bookmark size={16} fill={book.monitor !== "none" ? "currentColor" : "none"} aria-hidden="true" />
					</button>
					<span class="hidden h-0 basis-full md:block" aria-hidden="true"></span>
					<button
						type="button"
						onclick={searchAll}
						disabled={book.monitor === "none"}
						class="hidden h-11 items-center gap-2 rounded-lg bg-accent px-4 text-[14px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50 md:inline-flex"
					>
						<Radar size={16} aria-hidden="true" />
						{i18n.books_search_now()}
					</button>
					<button
						type="button"
						onclick={() => (manualOpen = true)}
						class="hidden h-11 items-center gap-2 rounded-lg border border-border-strong bg-white/[0.08] px-4 text-[14px] font-medium text-fg backdrop-blur-sm transition hover:bg-white/[0.14] md:inline-flex"
					>
						<Search size={16} aria-hidden="true" />
						{i18n.action_manual_search()}
					</button>
					<LibraryActions
						base={`/books/${book.id}`}
						title={book.title}
						media="books"
						queryKey="books"
						backHref="/books"
						hasFiles={book.formats.some((f) => !!f.file)}
						profile={book.quality_profile}
						removeBody={i18n.books_remove_body()}
						filesLabel={i18n.books_delete_files_label()}
					/>
				</div>
			{/if}
		{/snippet}
	</MediaHero>

	<div class={cn("w-full px-4 md:px-8 md:pb-8 md:pt-2", canEdit ? "pb-24" : "pb-8")}>
		<div class="grid gap-3 md:grid-cols-2 md:gap-4">
			{#each slots as slot (slot.format)}
				{@const edition = editionOf(slot.edition_id)}
				<FormatCard
					{slot}
					{edition}
					editionCount={book.editions.filter((e) => e.format === slot.format).length}
					preferred={book.preferred_language}
					noLocal={!!edition && edition.language !== book.preferred_language && !hasLocalEdition(book, slot.format)}
					{canEdit}
					onSearch={() => search(slot.format)}
					onChangeEdition={() => changeEdition(slot.format)}
					onMonitor={() => monitorFormat(slot.format)}
				/>
			{/each}
		</div>

		<div class="mt-8 grid gap-6 md:grid-cols-[1fr_260px] md:gap-7 lg:grid-cols-[1fr_320px] lg:gap-10">
			<section class="min-w-0" aria-labelledby="book-synopsis">
				<h2 id="book-synopsis" class={h3}>{i18n.detail_synopsis()}</h2>
				<p class="mt-3 max-w-[720px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
					{book.overview ?? i18n.detail_no_overview()}
				</p>
				{#if people.length > 0}
					<div class="my-5 h-px bg-border"></div>
					<h3 class="mb-3 {h3}">{i18n.books_contributors()}</h3>
					<PeopleGrid {people} dense />
				{/if}
			</section>
			<aside class="flex min-w-0 flex-col gap-4">
				<InfoCard id="book-details" title={i18n.common_details()} rows={detailRows} />
				<InfoCard id="book-library" title={i18n.nav_library()} rows={libraryRows} />
			</aside>
		</div>

		<div bind:this={editionsEl} class="mt-8">
			<EditionsTable editions={book.editions} {inUse} {filter} onFilterChange={(f) => (filter = f)} {canEdit} onUse={use} />
		</div>
	</div>

	{#if canEdit}
		<div
			class="fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+3.5rem)] z-30 flex items-center gap-2 border-t border-border bg-bg-elevated/95 px-3 pb-4 pt-2.5 backdrop-blur-md md:hidden"
			aria-label={i18n.books_actions()}
		>
			<button
				type="button"
				onclick={searchAll}
				disabled={book.monitor === "none"}
				class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent text-[14px] font-semibold text-fg-on-accent disabled:opacity-50"
			>
				<Radar size={16} aria-hidden="true" />
				{i18n.books_search_now()}
			</button>
			<button
				type="button"
				onclick={() => (manualOpen = true)}
				aria-label={i18n.action_manual_search()}
				title={i18n.action_manual_search()}
				class="grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border-strong bg-bg-elevated text-fg-muted transition active:bg-surface"
			>
				{#if downloading}
					<LoaderCircle size={18} class="animate-spin" aria-hidden="true" />
				{:else}
					<Search size={18} aria-hidden="true" />
				{/if}
			</button>
		</div>
	{/if}

	<Modal open={manualOpen} title={i18n.manual_search_scope({ scope: book.title })} size="4xl" onClose={() => (manualOpen = false)}>
		<ReleasesTable
			searchPath={`/books/${book.id}/releases`}
			grabPath={`/books/${book.id}/grab`}
			queryKey={["releases", "book", book.id]}
			existingCount={book.formats.filter((f) => !!f.file).length}
			enabled={manualOpen}
			onGrabbed={() => (manualOpen = false)}
			media="books"
		/>
	</Modal>

	<Dialog
		open={!!pending}
		title={i18n.books_switch_title()}
		size="lg"
		onClose={() => (pending = null)}
		actions={[
			{ label: i18n.common_cancel(), variant: "ghost" },
			{ label: i18n.books_use_edition(), variant: "primary", onClick: () => pending && applyEdition(pending), autofocus: true },
		]}
	>
		{#if pending && pendingSlot && pendingCurrent}
			<p class="text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
				{i18n.books_switch_body({ format: formatLabel(pending.format).toLowerCase() })}
			</p>
			<div class="mt-5 grid gap-3 sm:grid-cols-2">
				{#each [{ label: i18n.replace_current_file(), e: pendingCurrent, detail: [pendingCurrent.publisher, String(pendingCurrent.year), pendingSlot.file?.container, pendingSlot.file ? formatBytes(pendingSlot.file.size, "") : ""] }, { label: i18n.replace_replaced_by(), e: pending, detail: [pending.publisher, String(pending.year), editionDetail(pending)] }] as box (box.label)}
					<div class="rounded-lg border border-border bg-bg-deep/60 p-3.5">
						<p class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">{box.label}</p>
						<div class="mt-2 flex items-center gap-1.5">
							<LangChip code={box.e.language} active />
							{#if box.e.original}
								<span class="font-mono text-[10px] uppercase tracking-[0.08em] text-fg-faint">{i18n.books_original()}</span>
							{/if}
						</div>
						<p class="mt-1.5 text-[13.5px] text-fg">{box.e.title}</p>
						<p class="mt-0.5 font-mono text-[11px] text-fg-subtle">{box.detail.filter(Boolean).join(" · ")}</p>
					</div>
				{/each}
			</div>
		{/if}
	</Dialog>
{/if}
