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
	} = $props();

	const rowClass =
		"flex w-full items-center gap-3 border-b border-border px-3 py-2.5 text-left transition last:border-b-0 active:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-ring";
</script>

{#snippet body()}
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

	<ChevronRight size={15} class="shrink-0 text-fg-faint" aria-hidden="true" />
{/snippet}

{#if href}
	<a {href} class={rowClass}>{@render body()}</a>
{:else}
	<button type="button" onclick={onOpen} class={rowClass}>
		{@render body()}
	</button>
{/if}
