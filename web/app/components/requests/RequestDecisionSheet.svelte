<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Check, RotateCcw, X } from "@lucide/svelte";
	import { formatRelative } from "../../lib/dates";
	import {
		STATUS_META,
		kindLabel,
		requesterName,
	} from "../../lib/requests-touch";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import Select from "../forms/Select.svelte";
	import LookupDetailPanel from "../shared/LookupDetailPanel.svelte";
	import type {
		MediaRequest,
		QualityProfile,
		RequestMediaDetails,
	} from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// B1: one surface can change a request. The row says what needs deciding;
	// this says everything you need to decide it and holds both decisions, so
	// the quality override is always in front of you when you commit.
	let {
		request,
		detail,
		detailLoading = false,
		detailError = false,
		reviewer,
		profiles = [],
		profile,
		onProfileChange,
		busy = false,
		onClose,
		onApprove,
		onReject,
		onReopen,
	}: {
		request: MediaRequest | null;
		detail?: RequestMediaDetails;
		detailLoading?: boolean;
		detailError?: boolean;
		reviewer: boolean;
		profiles?: QualityProfile[];
		profile: string;
		onProfileChange: (v: string) => void;
		busy?: boolean;
		onClose: () => void;
		onApprove: (r: MediaRequest, profile: string) => void;
		onReject: (r: MediaRequest) => void;
		onReopen: (r: MediaRequest) => void;
	} = $props();

	$effect(() => {
		if (!request) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

	let meta = $derived(request ? STATUS_META[request.status] : null);
	let pending = $derived(request?.status === "pending");
	let options = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...profiles.map((p) => ({ value: p.name, label: p.name })),
	]);
</script>

{#if request && meta}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.requests_detail()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-start justify-between gap-3 border-b border-border px-5 pb-3.5 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<div class="min-w-0 pt-1">
					<h2 class="truncate text-[17px] font-semibold tracking-tight text-fg">
						{request.title}
					</h2>
					<p class="mt-1 font-mono text-[11px] text-fg-subtle">
						{detail?.year ? `${detail.year} · ` : ""}{kindLabel(
							request.media_type,
						)}
					</p>
				</div>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-3 pt-4"
			>
				<!-- What was asked, by whom, and where it landed. This is what the sheet
				     was opened to answer, so it leads; the artwork and synopsis below are
				     for judging the title itself. The decision sits at the head of the
				     list rather than as a pill in the header — for a rejection it reads
				     directly into the reason under it. -->
				<div class="pb-1">
					<div class="flex items-baseline justify-between gap-4 py-2 text-[13px]">
						<span class="shrink-0 text-fg-subtle">{i18n.common_decision()}</span>
						<span
							class="min-w-0 truncate text-right font-semibold"
							style:color="var(--status-{meta.token})"
						>
							{meta.label}
						</span>
					</div>

					<!-- The reason belongs to the decision, so it hangs directly off it
					     rather than sitting below the rest of the metadata. -->
					{#if request.status === "denied" && request.reason}
						<blockquote
							class="mb-1 border-l-2 border-status-failed/50 pl-3 text-[13px] leading-relaxed text-fg-muted"
						>
							{request.reason}
						</blockquote>
					{/if}

					{#each [{ k: i18n.requests_requested_by(), v: requesterName(request) }, { k: i18n.common_when(), v: formatRelative(request.created_at) }, { k: i18n.requests_asked_for(), v: request.quality_profile || i18n.quality_no_preference(), mono: true }, ...(request.approved_by ? [{ k: i18n.requests_decided_by(), v: `${request.approved_by.display_name || request.approved_by.email} · ${formatRelative(request.updated_at)}` }] : [])] as row (row.k)}
						<div class="flex items-baseline justify-between gap-4 py-2 text-[13px]">
							<span class="shrink-0 text-fg-subtle">{row.k}</span>
							<span
								class="min-w-0 truncate text-right text-fg {row.mono
									? 'font-mono text-[12px]'
									: ''}"
							>
								{row.v}
							</span>
						</div>
					{/each}
				</div>

				<div class="mt-3 border-t border-border pt-4">
					{#if detailError}
						<p class="text-[13px] text-fg-subtle">{i18n.requests_load_failed()}</p>
					{:else}
						<LookupDetailPanel
							kind={request.media_type === "tvshow" ? "series" : "movie"}
							item={{
								title: request.title,
								year: detail?.year,
								poster_url: detail?.poster_url,
								overview: detail?.overview,
							}}
							{detail}
							loading={detailLoading}
							showTitle={false}
							compact
						/>
					{/if}
				</div>

				{#if reviewer && pending}
					<div class="mt-3 border-t border-border pt-4">
						<div
							class="mb-2 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
						>
							{i18n.requests_add_with_profile()}
						</div>
						<Select
							id="qp-sheet-{request.id}"
							value={profile}
							{options}
							onChange={onProfileChange}
						/>
					</div>
				{/if}
			</div>

			{#if reviewer}
				<div
					class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
				>
					{#if pending}
						<button
							type="button"
							disabled={busy}
							onclick={() => onReject(request)}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
						>
							<X size={15} aria-hidden="true" />
							{i18n.common_reject()}
						</button>
						<button
							type="button"
							disabled={busy}
							onclick={() => onApprove(request, profile)}
							class="inline-flex h-11 flex-[1.4] items-center justify-center gap-2 rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed disabled:opacity-60"
						>
							<Check size={15} aria-hidden="true" />
							{i18n.requests_approve_add()}
						</button>
					{:else}
						<button
							type="button"
							disabled={busy}
							onclick={() => onReopen(request)}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
						>
							<RotateCcw size={15} aria-hidden="true" />
							{i18n.requests_reopen()}
						</button>
					{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}
