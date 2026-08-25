<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import ProgressRing from "./ProgressRing.svelte";
	import { cn } from "../../lib/cn";
	import type { MetaLine } from "../../lib/activity-touch";
	import type { StatusKind } from "../shared/StatusPill.svelte";

	// One row shape for all three touch views. Only the third line differs
	// between Queue, History and Torrents — the ring, title and release line are
	// the same object every time.
	let {
		status,
		progress = 0,
		title,
		release,
		meta,
		badge,
		placeholderTitle = false,
		href,
		onOpen,
		onResolve,
		resolveLabel,
	}: {
		status: StatusKind;
		progress?: number;
		title: string;
		release: string;
		meta: MetaLine;
		// "untracked" on a torrent with no library item behind it.
		badge?: string;
		// A magnet that hasn't resolved has no name yet, so the title is standing
		// in for one and shouldn't read as the release's own.
		placeholderTitle?: boolean;
		// A row that navigates rather than opening a sheet — the dashboard's
		// queue rows, which have no detail surface of their own. Routify
		// intercepts the click, so an <a> is all it takes.
		href?: string;
		onOpen?: () => void;
		// A held download is the only queue state whose next move belongs to a
		// person, so its row carries the action instead of a chevron. Given this,
		// the row becomes a div with a button inside rather than a button in a
		// button, and tapping the text still opens the detail.
		onResolve?: () => void;
		resolveLabel?: string;
	} = $props();

	const rowClass =
		"flex w-full items-center gap-3 border-b border-border px-3 py-2.5 text-left transition last:border-b-0 active:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-ring";
</script>

{#snippet lead()}
	<ProgressRing {status} {progress} />

	<span class="min-w-0 flex-1">
		<span class="flex min-w-0 items-center gap-1.5">
			<span
				class={cn(
					"truncate text-sm font-semibold tracking-[-0.01em]",
					placeholderTitle ? "italic text-fg-muted" : "text-fg",
				)}
			>
				{title}
			</span>
			{#if badge}
				<span
					class="shrink-0 rounded-full border border-border px-1.5 py-px text-[8.5px] font-semibold uppercase tracking-wide text-fg-subtle"
				>
					{badge}
				</span>
			{/if}
		</span>
		<span class="block truncate font-mono text-[11px] text-fg-subtle">
			{release}
		</span>
		<span
			class="block truncate font-mono text-[11px]"
			style:color={meta.color ?? "var(--fg-muted)"}
		>
			{meta.text || "—"}
		</span>
	</span>

{/snippet}

{#snippet body()}
	{@render lead()}
	<ChevronRight size={15} class="shrink-0 text-fg-faint" aria-hidden="true" />
{/snippet}

{#if onResolve}
	<div class={rowClass}>
		<button
			type="button"
			onclick={onOpen}
			class="flex min-w-0 flex-1 items-center gap-3 rounded-sm text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			{@render lead()}
		</button>
		<button
			type="button"
			onclick={onResolve}
			class="pill-action inline-flex h-11 shrink-0 items-center rounded-md px-4 text-[13px] font-semibold transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			style:--c="var(--status-{status})"
		>
			{resolveLabel}
		</button>
	</div>
{:else if href}
	<a {href} class={rowClass}>{@render body()}</a>
{:else}
	<button type="button" onclick={onOpen} class={rowClass}>
		{@render body()}
	</button>
{/if}

<style>
	/* Filled with the row's own status colour, dark text on it — the same
	   treatment StatusPill's solid variant uses, and for the same reason: the
	   utility class for a token added this late may not be generated. */
	.pill-action {
		background-color: var(--c);
		color: var(--bg-deep);
	}
	.pill-action:hover {
		filter: brightness(1.08);
	}
</style>
