<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { formatRelative } from "../../lib/dates";
	import {
		STATUS_META,
		TONE_CLASS,
		kindToken,
		outcomeWord,
		requesterName,
	} from "../../lib/requests-touch";
	import type { MediaRequest } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// A2: the row says which request this is and what it wants from you. Every
	// action lives in the sheet below md, so the list stays a list.
	let {
		request,
		reviewer = false,
		wide = false,
		mine = false,
		busy = false,
		onOpen,
		onApprove,
		onReject,
	}: {
		request: MediaRequest;
		reviewer?: boolean;
		// E2: on your own list every row would otherwise repeat your own name.
		mine?: boolean;
		// F1: from md up the same row gains the requested profile and the two
		// decisions as trailing columns, so one component serves 390 and 834.
		wide?: boolean;
		busy?: boolean;
		onOpen: (r: MediaRequest) => void;
		onApprove?: (r: MediaRequest) => void;
		onReject?: (r: MediaRequest) => void;
	} = $props();

	let meta = $derived(STATUS_META[request.status]);
	let word = $derived(outcomeWord(request, reviewer));
	let decidable = $derived(reviewer && request.status === "pending");
	let inline = $derived(wide && decidable);
</script>

<div class="flex items-center">
	<button
		type="button"
		onclick={() => onOpen(request)}
		class="flex min-w-0 flex-1 items-center gap-3 px-3.5 py-2.5 text-left transition active:bg-bg-card md:px-4"
	>
		<span
			class="h-2 w-2 shrink-0 rounded-full"
			style:background-color="var(--status-{kindToken(request.media_type)})"
			aria-hidden="true"
		></span>

		<span class="min-w-0 flex-1">
			<span class="block truncate text-[14.5px] font-medium tracking-[-0.01em] text-fg">
				{request.title}
			</span>
			<span class="mt-0.5 block truncate text-[12px] text-fg-subtle">
				{mine ? "" : requesterName(request) + " · "}{formatRelative(request.created_at)}
			</span>
		</span>

		{#if wide}
			<span
				class="hidden w-[104px] shrink-0 truncate font-mono text-[11.5px] text-fg-subtle md:block"
			>
				{request.quality_profile || i18n.quality_no_preference()}
			</span>
		{/if}

		<span
			class={cn(
				"shrink-0 whitespace-nowrap text-[12.5px] font-semibold",
				TONE_CLASS[word.tone],
				// From md up a wide row states its status once, in the trailing column:
				// the two buttons on a pending row, the pill on a decided one. Keeping
				// this word as well printed "Approved" twice, side by side.
				wide && "md:hidden",
			)}
		>
			{word.text}
		</span>

		<!-- Below md the chevron is what says the row opens something. From md up
		     the buttons beside it make that obvious, and a chevron next to two
		     explicit actions is one affordance too many. -->
		<ChevronRight
			size={14}
			class={cn("shrink-0 text-fg-faint", wide && "md:hidden")}
			aria-hidden="true"
		/>
	</button>

	{#if inline}
		<!-- Approve here commits the profile the requester asked for — the same
		     value the sheet pre-selects — so the row and the sheet cannot
		     disagree about what "Approve" means. -->
		<div class="hidden shrink-0 items-center gap-2 pr-4 md:flex">
			<button
				type="button"
				disabled={busy}
				onclick={() => onReject?.(request)}
				class="inline-flex h-11 items-center rounded-lg border border-border px-3.5 text-[13px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:opacity-60"
			>
				{i18n.common_reject()}
			</button>
			<button
				type="button"
				disabled={busy}
				onclick={() => onApprove?.(request)}
				class="inline-flex h-11 items-center rounded-lg bg-accent px-4 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
			>
				{i18n.requests_approve_add()}
			</button>
		</div>
	{:else if wide}
		<!-- The word the row would have shown, in the column the buttons occupy on a
		     pending row — so "In review" and "Ready to watch" survive for the
		     requester rather than reverting to the raw status label. -->
		<span
			class="mr-4 hidden shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold md:inline-flex"
			style:color="var(--status-{meta.token})"
			style:background-color="color-mix(in srgb, var(--status-{meta.token}) 16%, transparent)"
		>
			{word.text}
		</span>
	{/if}
</div>
