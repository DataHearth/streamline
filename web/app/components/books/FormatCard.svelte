<script lang="ts">
	import { BookOpen, Eye, Headphones, Info, Search } from "@lucide/svelte";
	import { formatBytes } from "@lib/format";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import LangChip from "./LangChip.svelte";
	import { editionDetail, formatLabel, languageName, type Edition, type FormatSlot } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What the book has in one format: the edition it follows, the file if
	// there is one, and the one or two things to do about it.
	let {
		slot,
		edition,
		editionCount,
		preferred,
		noLocal = false,
		canEdit = false,
		onSearch,
		onChangeEdition,
		onMonitor,
	}: {
		slot: FormatSlot;
		edition: Edition | undefined;
		editionCount: number;
		preferred: string;
		noLocal?: boolean;
		canEdit?: boolean;
		onSearch: () => void;
		onChangeEdition: () => void;
		onMonitor: () => void;
	} = $props();

	let pill = $derived.by(() => {
		if (slot.state === "available") return { token: "available", label: i18n.books_in_library(), live: false };
		if (slot.state === "downloading")
			return { token: "downloading", label: `${i18n.status_downloading()} · ${slot.progress ?? 0}%`, live: true };
		if (slot.state === "wanted") return { token: "wanted", label: i18n.status_wanted(), live: false };
		return { token: "missing", label: i18n.books_not_monitored(), live: false };
	});
	let facts = $derived.by(() => {
		if (!edition) return "";
		const p: string[] = [edition.publisher, String(edition.year)];
		if (slot.state === "available" && slot.file) p.push(slot.file.container, formatBytes(slot.file.size, ""));
		else {
			const d = editionDetail(edition);
			if (d) p.push(d);
		}
		return p.filter(Boolean).join(" · ");
	});
	const ghost =
		"inline-flex h-11 items-center gap-2 rounded-lg border border-border bg-bg-elevated/80 px-3 text-[13px] font-medium text-fg transition hover:border-border-strong lg:h-9";
</script>

<div class="rounded-lg border border-border bg-bg-elevated/70 p-4">
	<div class="flex items-center gap-2.5">
		<span class="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-surface-2 text-fg-muted">
			{#if slot.format === "ebook"}
				<BookOpen size={16} aria-hidden="true" />
			{:else}
				<Headphones size={16} aria-hidden="true" />
			{/if}
		</span>
		<h3 class="text-[14px] font-semibold text-fg">{formatLabel(slot.format)}</h3>
		<span class="ml-auto">
			<LabelPill token={pill.token} label={pill.label} size="md" variant="translucent" live={pill.live} />
		</span>
	</div>

	{#if slot.state === "unmonitored"}
		<p class="mt-3 text-[13.5px] text-fg-muted">
			{editionCount > 0
				? (editionCount === 1 ? i18n.books_editions_exist_one : i18n.books_editions_exist_other)({ count: String(editionCount) })
				: i18n.books_no_editions()}
		</p>
		{#if canEdit && editionCount > 0}
			<div class="mt-4">
				<button type="button" onclick={onMonitor} class={ghost}>
					<Eye size={15} class="text-fg-subtle" aria-hidden="true" />
					{slot.format === "ebook" ? i18n.books_monitor_ebook_action() : i18n.books_monitor_audiobook_action()}
				</button>
			</div>
		{/if}
	{:else if edition}
		<div class="mt-3 flex min-w-0 items-center gap-2">
			<LangChip code={edition.language} active />
			{#if edition.original}
				<span class="font-mono text-[10px] uppercase tracking-[0.08em] text-fg-faint">{i18n.books_original()}</span>
			{/if}
			<p class="min-w-0 truncate text-[13.5px] text-fg">{edition.title}</p>
		</div>
		<p class="mt-1.5 font-mono text-[11.5px] text-fg-subtle">{facts}</p>
		{#if slot.replacing}
			<div class="mt-3 flex items-start gap-2 rounded-md bg-surface px-3 py-2.5 text-[12.5px] leading-snug text-fg-muted">
				<Info size={14} class="mt-px shrink-0 text-fg-subtle" aria-hidden="true" />
				<span>{i18n.books_replacing_note({ language: languageName(slot.replacing.language) })}</span>
			</div>
		{/if}
		{#if noLocal}
			<div class="mt-3 flex items-start gap-2 rounded-md bg-surface px-3 py-2.5 text-[12.5px] leading-snug text-fg-muted">
				<Info size={14} class="mt-px shrink-0 text-fg-subtle" aria-hidden="true" />
				<span>{i18n.books_no_local_edition({ language: languageName(preferred), original: languageName(edition.language) })}</span>
			</div>
		{/if}
		{#if canEdit}
			<div class="mt-4 flex flex-wrap gap-2">
				{#if slot.state === "wanted"}
					<button
						type="button"
						onclick={onSearch}
						class="inline-flex h-11 items-center gap-2 rounded-lg bg-accent px-3 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover lg:h-9"
					>
						<Search size={15} aria-hidden="true" />
						{i18n.action_manual_search()}
					</button>
				{/if}
				{#if editionCount > 1}
					<button type="button" onclick={onChangeEdition} class={ghost}>{i18n.books_change_edition()}</button>
				{/if}
			</div>
		{/if}
	{/if}
</div>
