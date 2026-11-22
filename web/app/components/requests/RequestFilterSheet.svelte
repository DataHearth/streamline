<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { twMerge } from "tailwind-merge";
	import {
		KIND_CHIPS,
		kindColor,
		statusChips,
		type RequestKind,
		type RequestTab,
	} from "@lib/requests-touch";
	import type { RequestCounts } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Everything the line above can't afford to keep on screen: the four
	// status tabs that overflow their box at 358px, and the media-type group
	// that can only wrap onto a second row beneath them.
	let {
		open,
		onClose,
		tab,
		onTabChange,
		kind,
		onKindChange,
		counts,
		resultCount,
		activeCount = 0,
		onReset,
	}: {
		open: boolean;
		onClose: () => void;
		tab: RequestTab;
		onTabChange: (t: RequestTab) => void;
		kind: RequestKind;
		onKindChange: (k: RequestKind) => void;
		counts: RequestCounts;
		resultCount: number;
		activeCount?: number;
		onReset: () => void;
	} = $props();


	const chip =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff = "border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 eyebrow text-[9.5px] text-fg-subtle";

	let chips = $derived(statusChips(counts));
	// The same dot the rows carry, so a chip and the rows it keeps match.
	const KIND_DOT: Record<string, string> = {
		movies: kindColor("movie"),
		series: kindColor("tvshow"),
		music: kindColor("artist"),
		books: kindColor("book"),
	};
</script>

{#if open}
	<BottomSheet
		title={i18n.filter_label()}
		label={i18n.requests_filter()}
		{onClose}
		bodyClass="pt-3"
	>
		<div class={label}>{i18n.common_status()}</div>
		<div class="flex flex-wrap gap-2">
			{#each chips as c (c.key)}
				{@const on = tab === c.key}
				<button
					type="button"
					aria-pressed={on}
					onclick={() => onTabChange(c.key)}
					class={twMerge(chip, on ? chipOn : chipOff)}
				>
					{c.label}
					{#if c.count !== undefined}
						<span class="font-mono text-[11px] tabular-nums opacity-70">
							{c.count}
						</span>
					{/if}
				</button>
			{/each}
		</div>

		<div class="pt-5">
			<div class={label}>{i18n.imports_media_type()}</div>
			<div class="flex flex-wrap gap-2">
				{#each KIND_CHIPS as k (k.key)}
					{@const on = kind === k.key}
					<button
						type="button"
						aria-pressed={on}
						onclick={() => onKindChange(k.key)}
						class={twMerge(chip, on ? chipOn : chipOff)}
					>
						{#if KIND_DOT[k.key]}
							<span
								class="h-1.5 w-1.5 shrink-0 rounded-full"
								style:background-color={KIND_DOT[k.key]}
								aria-hidden="true"
							></span>
						{/if}
						{k.label}
					</button>
				{/each}
			</div>
		</div>
		{#snippet footer()}
			<button
				type="button"
				disabled={activeCount === 0}
				onclick={onReset}
				class="inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40"
			>
				{i18n.common_reset()}
			</button>
			<button
				type="button"
				onclick={onClose}
				class="inline-flex h-11 flex-1 items-center justify-center rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed"
			>
				{i18n.common_show_n({ n: resultCount })}
			</button>
		{/snippet}
	</BottomSheet>
{/if}
