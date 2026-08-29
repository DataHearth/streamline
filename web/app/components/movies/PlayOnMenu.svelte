<script lang="ts">
	import {
		Play,
		ChevronDown,
		ExternalLink,
		ServerOff,
		TriangleAlert,
		X,
	} from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { api } from "../../lib/api";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import BrandLogo from "../settings/BrandLogo.svelte";
	import type { PlayOnLink, PlayOnLinkList } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		// Play-on endpoint (movie or series) returning a PlayOnLinkList.
		path: string;
		queryKey: readonly unknown[];
		disabled?: boolean;
		disabledTitle?: string;
		// Icon-only trigger, for a bar that already has a wide primary action.
		compact?: boolean;
		// Wide accent trigger: the primary action of the phone action bar.
		primary?: boolean;
	};
	let {
		path,
		queryKey,
		disabled = false,
		disabledTitle,
		compact = false,
		primary = false,
	}: Props = $props();

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);

	const q = createQuery<PlayOnLinkList>(() => ({
		queryKey,
		queryFn: () => api<PlayOnLinkList>(path),
		enabled: open && !disabled,
	}));

	function toggle() {
		if (disabled) return;
		open = !open;
	}
	function close() {
		open = false;
		triggerEl?.focus();
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === "Escape") close();
	}

	let links = $derived(q.data?.items ?? []);

	// The phone action bar carries backdrop-blur, which makes it a containing
	// block for fixed positioning — a sheet declared inside it would anchor to
	// the bar, not the viewport. Same portal the kebab menus and modals use.
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() {
				node.parentNode?.removeChild(node);
			},
		};
	}

	// Escape and the scroll lock belong to the document once the sheet is
	// portalled out of the wrapper below.
	$effect(() => {
		if (!open || !primary) return;
		lockScroll();
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

{#snippet row(l: PlayOnLink, big: boolean)}
	<span class="flex min-w-0 items-center {big ? 'gap-3' : 'gap-2.5'}">
		<BrandLogo name={l.server_type} size={big ? 22 : 18} />
		<span class="flex min-w-0 flex-col">
			<span class="truncate font-medium text-fg {big ? 'text-[15px]' : ''}">
				{l.name}
			</span>
			{#if l.status === "fallback"}
				<span class="text-[10px] text-fg-muted">{i18n.playon_library_fallback()}</span>
			{:else if l.status === "unavailable"}
				<span class="text-[10px] text-fg-faint">{i18n.playback_unavailable()}</span>
			{/if}
		</span>
	</span>
	{#if l.status !== "unavailable"}
		<ExternalLink
			class="{big ? 'h-[18px] w-[18px]' : 'h-4 w-4'} shrink-0 text-fg-muted"
			aria-hidden="true"
		/>
	{/if}
{/snippet}

{#snippet body(big: boolean)}
	{#if q.isLoading}
		<p class="text-fg-muted {big ? 'px-5 py-5 text-sm' : 'px-3 py-3 text-xs'}">
			{i18n.common_resolving()}
		</p>
	{:else if q.isError}
		<div
			role="alert"
			title={q.error?.message}
			class="flex flex-col gap-1.5 text-status-failed {big
				? 'px-5 py-5 text-sm'
				: 'px-3 py-3 text-xs'}"
		>
			<span class="flex items-start gap-2">
				<TriangleAlert
					class="mt-px h-3.5 w-3.5 shrink-0"
					aria-hidden="true"
				/>
				{i18n.playback_load_failed()}
			</span>
			<button
				type="button"
				onclick={() => q.refetch()}
				class="self-start rounded font-medium underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				{i18n.common_retry()}
			</button>
		</div>
	{:else if links.length === 0}
		<div class="grid place-items-center gap-1 {big ? 'px-5 py-9' : 'px-3 py-4'}">
			<ServerOff
				class="{big ? 'h-6 w-6' : 'h-5 w-5'} text-fg-faint"
				aria-hidden="true"
			/>
			<p class="text-fg-muted {big ? 'text-sm' : 'text-xs'}">
				{i18n.mediaserver_none()}
			</p>
			<a
				href="/settings/media-servers"
				class="text-accent hover:underline {big ? 'text-sm' : 'text-xs'}"
			>
				{i18n.playon_configure_servers()}
			</a>
		</div>
	{:else}
		<ul>
			{#each links as l (l.name)}
				<li class={big ? "border-b border-border last:border-0" : ""}>
					{#if l.url}
						<a
							href={l.url}
							target="_blank"
							rel="noopener noreferrer"
							role="menuitem"
							onclick={close}
							class="flex w-full items-center justify-between gap-2 text-left {big
								? 'px-5 py-4 text-[15px] active:bg-bg-hover'
								: 'px-3 py-2 text-sm hover:bg-bg-hover'}"
						>
							{@render row(l, big)}
						</a>
					{:else}
						<div
							role="menuitem"
							aria-disabled="true"
							class="flex w-full items-center justify-between gap-2 text-left opacity-50 {big
								? 'px-5 py-4 text-[15px]'
								: 'px-3 py-2 text-sm'}"
						>
							{@render row(l, big)}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
{/snippet}

<div
	class={primary ? "relative flex min-w-0 flex-1" : "relative"}
	onkeydown={onKey}
	role="presentation"
>
	{#if primary}
		<button
			bind:this={triggerEl}
			type="button"
			aria-haspopup="dialog"
			aria-expanded={open}
			{disabled}
			title={disabled ? disabledTitle : undefined}
			onclick={toggle}
			class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent px-4 text-sm font-semibold text-fg-on-accent transition active:bg-accent-pressed disabled:cursor-not-allowed disabled:bg-surface-2 disabled:text-fg-faint"
		>
			<Play class="h-[18px] w-[18px]" aria-hidden="true" />
			{i18n.action_play_on()}
		</button>
	{:else if compact}
		<button
			bind:this={triggerEl}
			type="button"
			aria-haspopup="menu"
			aria-expanded={open}
			aria-label={i18n.action_play_on()}
			{disabled}
			title={disabled ? disabledTitle : i18n.action_play_on()}
			onclick={toggle}
			class="grid h-11 w-11 place-items-center rounded-lg border border-border-strong bg-bg-elevated text-fg transition focus:outline-none focus:ring-2 focus:ring-accent/40 disabled:cursor-not-allowed disabled:border-border disabled:text-fg-faint"
		>
			<Play class="h-[18px] w-[18px] text-accent" aria-hidden="true" />
		</button>
	{:else}
		<button
			bind:this={triggerEl}
			type="button"
			aria-haspopup="menu"
			aria-expanded={open}
			{disabled}
			title={disabled ? disabledTitle : undefined}
			onclick={toggle}
			class="inline-flex w-[220px] items-center justify-between gap-2 rounded-md border border-border-strong bg-bg-elevated px-3 py-2 text-sm font-medium text-fg hover:border-accent/60 focus:outline-none focus:ring-2 focus:ring-accent/40 disabled:cursor-not-allowed disabled:border-border disabled:text-fg-faint disabled:hover:border-border"
		>
			<span class="inline-flex items-center gap-2">
				<Play class="h-4 w-4 text-accent" aria-hidden="true" />
				{i18n.action_play_on()}
			</span>
			<ChevronDown
				class="h-4 w-4 text-fg-muted transition {open ? 'rotate-180' : ''}"
				aria-hidden="true"
			/>
		</button>
	{/if}

	{#if open && !primary}
		<div
			role="menu"
			aria-live="polite"
			transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
			class="absolute right-0 z-30 w-[260px] overflow-hidden rounded-md border border-border bg-bg-elevated shadow-3 {compact
				? 'bottom-full mb-2'
				: 'mt-1'}"
		>
			{@render body(false)}
		</div>
		<button
			type="button"
			aria-hidden="true"
			tabindex="-1"
			class="fixed inset-0 z-20 cursor-default"
			onclick={close}
		></button>
	{/if}
</div>

<!-- Phone: the same links as a bottom sheet. A right-aligned popover anchored to
     a full-width trigger ran off the left edge of a 390px viewport, and every
     other list a phone opens in this app is a sheet. -->
{#if open && primary}
	<div
		use:portal
		class="fixed inset-0 z-50 md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.action_play_on()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={close}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: close }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[80dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-3 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{i18n.action_play_on()}</h2>
				<button
					type="button"
					onclick={close}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				aria-live="polite"
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain border-t border-border pb-[max(env(safe-area-inset-bottom),8px)]"
			>
				{@render body(true)}
			</div>
		</div>
	</div>
{/if}
