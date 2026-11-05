<script lang="ts">
	import { untrack } from "svelte";
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { FolderUp, Files, Upload, X } from "@lucide/svelte";
	import Modal from "@components/modals/Modal.svelte";
	import ReleaseFilesView from "@components/shared/ReleaseFilesView.svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import type { BookFormat, DiscographyFiles, ReleaseFiles } from "@lib/music-books";
	import { toast } from "@lib/toast";
	import {
		entriesFromDrop,
		entriesFromInput,
		folderName,
		uploadFiles,
		withoutFolders,
		type UploadEntry,
	} from "@lib/upload";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Files onto a library item: an album, one of its tracks, an artist's
	// discography or a book. Picked or dropped files are read against the
	// target first (the server matches the names, nothing is sent yet), then
	// streamed in one multipart request that the importer picks up as a
	// finished download. Mounted only while open; onDone closes it, sent or not.
	let {
		target,
		id,
		slot,
		hasFiles,
		slotsHolding,
		onDone,
		initial = [],
	}: {
		target: "album" | "track" | "artist" | "book";
		id: number;
		// A book's slot, when the caller already knows it; otherwise chosen here.
		slot?: BookFormat;
		hasFiles: boolean;
		// A book replaces per slot, so whether it holds a file depends on the slot.
		slotsHolding?: BookFormat[];
		onDone: () => void;
		// Files already in hand, a file dropped onto a track row.
		initial?: UploadEntry[];
	} = $props();

	const MAX_FILES = 5000;
	const qc = useQueryClient();

	type BookPreview = { files: { path: string; size: number; outcome: "kept" | "ignored" | "refused" }[] };
	type AlbumPreview = ReleaseFiles & { tracks_pending?: boolean };

	let entries = $state.raw<UploadEntry[]>(untrack(() => initial));
	let title = $state<string | undefined>();
	let replace = $state(false);
	let chosenSlot = $state<BookFormat>(untrack(() => slot ?? "ebook"));
	let over = $state(false);
	let sending = $state(false);
	let sent = $state({ done: 0, total: 0 });
	let failure = $state<unknown>(null);
	let controller: AbortController | null = null;
	let folderInput = $state<HTMLInputElement | null>(null);
	let filesInput = $state<HTMLInputElement | null>(null);

	const heading = $derived(
		{
			album: i18n.upload_title_album,
			track: i18n.upload_title_track,
			artist: i18n.upload_title_artist,
			book: i18n.upload_title_book,
		}[target](),
	);
	const base = $derived(
		{ album: `/music/albums/${id}`, track: `/music/tracks/${id}`, artist: `/music/artists/${id}`, book: `/books/${id}` }[
			target
		],
	);
	const holdsFiles = $derived(slotsHolding ? slotsHolding.includes(chosenSlot) : hasFiles);
	const totalSize = $derived(entries.reduce((n, e) => n + e.file.size, 0));
	const tooMany = $derived(entries.length > MAX_FILES);
	const wrongCount = $derived(target === "track" && entries.length > 1);
	const previewable = $derived(target !== "track" && entries.length > 0 && !tooMany);

	// The key names the selection by its paths and sizes; the list itself can be
	// thousands long.
	const selectionKey = $derived.by(() => {
		let h = 5381;
		for (const e of entries) {
			const s = `${e.path}:${e.file.size}`;
			for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
		}
		return `${entries.length}:${h}`;
	});

	const preview = createQuery(() => ({
		queryKey: ["upload-preview", target, id, target === "book" ? chosenSlot : "", selectionKey],
		queryFn: () =>
			api<AlbumPreview | DiscographyFiles | BookPreview>(
				`${base}/upload/preview${target === "book" ? `?slot=${chosenSlot}` : ""}`,
				{ method: "POST", body: { files: entries.map((e) => ({ path: e.path, size: e.file.size })) } },
			),
		enabled: previewable,
		retry: false,
		refetchOnWindowFocus: false,
		staleTime: 60_000,
	}));

	const album = $derived(target === "album" ? (preview.data as AlbumPreview | undefined) : undefined);
	const artist = $derived(target === "artist" ? (preview.data as DiscographyFiles | undefined) : undefined);
	const book = $derived(target === "book" ? (preview.data as BookPreview | undefined) : undefined);
	const skippedFolders = $derived(artist?.folders.filter((f) => !f.album_id).map((f) => f.path) ?? []);

	// Send waits for the preview and needs something it says would import.
	const wouldImport = $derived.by(() => {
		if (entries.length === 0 || tooMany || wrongCount) return false;
		if ((target === "track" || target === "book") && holdsFiles && !replace) return false;
		if (target === "track") return true;
		if (preview.isPending || preview.isError || !preview.data) return false;
		if (album) return !!album.tracks_pending || album.files.some((f) => f.track_id);
		if (artist) return artist.files.some((f) => f.album_id);
		// The server refuses a whole upload over one refused file, after reading it.
		return !!book && book.files.some((f) => f.outcome === "kept") && !book.files.some((f) => f.outcome === "refused");
	});

	function select(next: UploadEntry[], name?: string) {
		entries = next;
		title = name;
		failure = null;
	}
	async function onDrop(e: DragEvent) {
		e.preventDefault();
		over = false;
		if (sending || !e.dataTransfer) return;
		// The name and the entries are both read before the first await.
		const name = folderName(e.dataTransfer);
		select(await entriesFromDrop(e.dataTransfer, target === "artist"), name);
	}
	function onPick(e: Event & { currentTarget: HTMLInputElement }) {
		const files = e.currentTarget.files;
		if (files && files.length > 0) select(entriesFromInput(files, target === "artist"), folderName(files));
		e.currentTarget.value = "";
	}

	async function send() {
		sending = true;
		failure = null;
		sent = { done: 0, total: 0 };
		controller = new AbortController();
		const q = new URLSearchParams();
		if (replace && holdsFiles) q.set("replace_existing", "true");
		if (title) q.set("title", title);
		if (target === "book") q.set("slot", chosenSlot);
		const body = target === "artist" ? withoutFolders(entries, skippedFolders) : entries;
		try {
			const res = await uploadFiles(
				`${base}/upload?${q}`,
				body,
				(done, total) => (sent = { done, total }),
				controller.signal,
			);
			const left = res.skipped_folders?.length ?? 0;
			toast.ok(left > 0 ? i18n.upload_done_skipped({ count: left }) : i18n.upload_done());
			for (const queryKey of [[target === "book" ? "books" : "music"], ["activity", "queue"]])
				qc.invalidateQueries({ queryKey });
			onDone();
		} catch (e) {
			if (!(e instanceof DOMException && e.name === "AbortError")) failure = e;
		} finally {
			sending = false;
			controller = null;
		}
	}

	function close() {
		controller?.abort();
		onDone();
	}

	const OUTCOME: Record<string, { label: () => string; tone: string }> = {
		kept: { label: i18n.upload_book_kept, tone: "text-status-available" },
		ignored: { label: i18n.upload_book_ignored, tone: "text-fg-subtle" },
		refused: { label: i18n.upload_book_refused, tone: "text-status-failed" },
	};
	const btn =
		"inline-flex h-11 items-center gap-2 rounded-lg border border-border-strong bg-white/[0.06] px-3 text-[13px] font-medium text-fg transition hover:bg-white/[0.12] disabled:opacity-50 lg:h-9";
</script>

<Modal open title={heading} size="lg" onClose={close}>
	<div class="flex flex-col gap-4">
		{#if target === "book" && !slot}
			<div role="radiogroup" aria-label={i18n.upload_slot()} class="flex items-center gap-2">
				<span class="font-mono text-[10.5px] uppercase tracking-[0.1em] text-fg-subtle">{i18n.upload_slot()}</span>
				{#each ["ebook", "audiobook"] as const as s (s)}
					<button
						type="button"
						role="radio"
						aria-checked={chosenSlot === s}
						disabled={sending}
						onclick={() => (chosenSlot = s)}
						class={cn(
							"h-9 rounded-lg border px-3 text-[13px] font-medium transition",
							chosenSlot === s ? "border-accent bg-accent/15 text-fg" : "border-border text-fg-muted hover:text-fg",
						)}
					>
						{s === "ebook" ? i18n.upload_slot_ebook() : i18n.upload_slot_audiobook()}
					</button>
				{/each}
			</div>
		{/if}

		<div
			role="group"
			ondragover={(e) => {
				e.preventDefault();
				over = true;
			}}
			ondragleave={() => (over = false)}
			ondrop={onDrop}
			class={cn(
				"flex flex-col items-center gap-3 rounded-lg border border-dashed px-4 py-6 text-center transition",
				over ? "border-accent bg-accent/10" : "border-border-strong",
			)}
		>
			<Upload size={22} class="text-fg-subtle" aria-hidden="true" />
			<p class="text-[13px] text-fg-muted">{target === "track" ? i18n.upload_drop_track() : i18n.upload_drop()}</p>
			<div class="flex flex-wrap items-center justify-center gap-2">
				{#if target !== "track"}
					<button type="button" class={btn} disabled={sending} onclick={() => folderInput?.click()}>
						<FolderUp size={15} aria-hidden="true" />
						{i18n.upload_pick_folder()}
					</button>
				{/if}
				<button type="button" class={btn} disabled={sending} onclick={() => filesInput?.click()}>
					<Files size={15} aria-hidden="true" />
					{target === "track" ? i18n.upload_pick_file() : i18n.upload_pick_files()}
				</button>
			</div>
			<input bind:this={folderInput} type="file" multiple hidden onchange={onPick} {...{ webkitdirectory: true }} />
			<input bind:this={filesInput} type="file" multiple={target !== "track"} hidden onchange={onPick} />
		</div>

		{#if entries.length > 0}
			<div class="flex items-center justify-between gap-3 text-[12.5px] text-fg-muted">
				<span class="min-w-0 truncate">
					{target === "track" && entries.length === 1
						? `${entries[0]?.path} · ${formatBytes(totalSize)}`
						: i18n.upload_selected({ count: entries.length, size: formatBytes(totalSize) })}
				</span>
				<button
					type="button"
					disabled={sending}
					onclick={() => select([])}
					class="inline-flex shrink-0 items-center gap-1 text-fg-subtle transition hover:text-fg disabled:opacity-50"
				>
					<X size={13} aria-hidden="true" />
					{i18n.upload_clear()}
				</button>
			</div>

			{#if tooMany}
				<p class="text-[12.5px] text-status-failed">{i18n.upload_too_many()}</p>
			{:else if wrongCount}
				<p class="text-[12.5px] text-status-failed">{i18n.upload_track_one()}</p>
			{:else if album?.tracks_pending}
				<p class="rounded-md border border-border bg-bg-base px-3 py-2.5 text-[12px] text-fg-muted">{i18n.upload_tracks_pending()}</p>
			{:else if target === "album" || target === "artist"}
				<ReleaseFilesView
					raw={album ?? artist}
					kind={target === "album" ? "tracks" : "discography"}
					variant="upload"
					pending={preview.isPending}
					error={preview.error}
				/>
			{:else if target === "book"}
				<div class="rounded-md border border-border bg-bg-base px-3 py-2.5 text-[12px]">
					{#if preview.isPending}
						<p class="text-fg-subtle">{i18n.release_files_loading()}</p>
					{:else if preview.isError}
						<p class="text-fg-muted">{errorText(preview.error)}</p>
					{:else if book}
						{#if !book.files.some((f) => f.outcome === "kept")}
							<p class="font-medium text-status-failed">{i18n.upload_book_none()}</p>
						{/if}
						<ul class="mt-1 flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain">
							{#each book.files as f (f.path)}
								<li class="flex items-baseline gap-3">
									<span class="min-w-0 flex-1 break-all font-mono text-[11px] text-fg">{f.path}</span>
									<span class={cn("shrink-0 text-[11px]", OUTCOME[f.outcome]?.tone)}>{OUTCOME[f.outcome]?.label()}</span>
									<span class="shrink-0 font-mono text-[10.5px] tabular text-fg-faint">{formatBytes(f.size)}</span>
								</li>
							{/each}
						</ul>
						<p class="mt-2 text-[11px] text-fg-faint">{i18n.upload_book_hint()}</p>
					{/if}
				</div>
			{/if}
		{/if}

		{#if holdsFiles}
			<label class="flex cursor-pointer items-center gap-2.5 text-[13px] text-fg">
				<input type="checkbox" bind:checked={replace} disabled={sending} class="h-4 w-4 cursor-pointer rounded accent-[var(--accent)]" />
				{target === "track" ? i18n.upload_replace_track() : i18n.upload_replace()}
			</label>
		{/if}

		{#if sending}
			<div>
				<div class="h-1.5 overflow-hidden rounded-full bg-surface" role="progressbar" aria-valuemin={0} aria-valuemax={sent.total} aria-valuenow={sent.done}>
					<div class="h-full bg-accent transition-[width]" style:width={sent.total > 0 ? `${(100 * sent.done) / sent.total}%` : "0%"}></div>
				</div>
				{#if sent.total > 0}
					<p class="mt-1 font-mono text-[11px] text-fg-faint">
						{i18n.upload_progress({ sent: formatBytes(sent.done), total: formatBytes(sent.total) })}
					</p>
				{/if}
			</div>
		{/if}

		{#if failure}
			<p role="alert" class="text-[13px] text-status-failed">{errorText(failure)}</p>
		{/if}
	</div>

	{#snippet footer()}
		<button type="button" onclick={close} class={btn}>{i18n.common_cancel()}</button>
		<button
			type="button"
			onclick={send}
			disabled={!wouldImport || sending}
			class="inline-flex h-11 items-center gap-2 rounded-lg bg-accent px-4 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50 lg:h-9"
		>
			<Upload size={15} aria-hidden="true" />
			{sending ? i18n.upload_sending() : i18n.upload_send()}
		</button>
	{/snippet}
</Modal>
