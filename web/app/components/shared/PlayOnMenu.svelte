<script lang="ts">
	import { tick } from "svelte";
	import { twMerge } from "tailwind-merge";
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import { closeOnBack } from "@lib/close-on-back.svelte";
	import {
		Play,
		ChevronDown,
		ExternalLink,
		ServerOff,
		TriangleAlert,
	} from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import BrandLogo from "@components/settings/BrandLogo.svelte";
	import type { PlayOnLink, PlayOnLinkList } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		// Play-on endpoint (movie or series) returning a PlayOnLinkList.
		path: string;
		queryKey: readonly unknown[];
		disabled?: boolean;
		disabledTitle?: string;
		// Icon-only phone trigger, for a row that already has a wide primary action.
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
	// Both phone triggers open the sheet, which handles its own Back.
	let sheet = $derived(primary || compact);
	closeOnBack(() => open && !sheet, () => close());
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

	let links = $derived(q.data?.items ?? []);

	// The dropdown is portalled to <body>, so focus has to be moved into it, once
	// the links are there. A focus the user has moved elsewhere is left alone.
	let items = $state<(HTMLElement | null)[]>([]);
	$effect(() => {
		if (!open || sheet || q.isLoading) return;
		void tick().then(() => {
			const at = document.activeElement;
			if (at !== triggerEl && at !== document.body) return;
			items[0]?.focus({ preventScroll: true });
		});
	});

	function onItemKey(e: KeyboardEvent, i: number) {
		const n = Math.max(1, links.length);
		let next: number;
		if (e.key === "ArrowDown") next = (i + 1) % n;
		else if (e.key === "ArrowUp") next = (i - 1 + n) % n;
		else if (e.key === "Home") next = 0;
		else if (e.key === "End") next = n - 1;
		else if (e.key === "Tab" && !sheet) {
			e.preventDefault();
			close();
			return;
		} else return;
		e.preventDefault();
		items[next]?.focus();
	}

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
</script>

{#snippet row(l: PlayOnLink, big: boolean)}
	<span class="flex min-w-0 items-center {big ? 'gap-3' : 'gap-2.5'}">
		<BrandLogo name={l.server_type} size={big ? 22 : 18} />
		<span class="flex min-w-0 flex-col">
			<span class="truncate text-fg {big ? 'text-[15px] font-medium' : ''}">
				{l.name}
			</span>
			{#if l.status === "fallback"}
				<span class="text-[10px] text-fg-muted">{i18n.playon_library_fallback()}</span>
			{:else if l.status === "unavailable"}
				<span class="text-[10px] text-fg-subtle">{i18n.playback_unavailable()}</span>
			{/if}
		</span>
	</span>
	{#if l.status !== "unavailable"}
		<ExternalLink
			class="{big ? 'h-[18px] w-[18px]' : 'h-3.5 w-3.5'} shrink-0 text-fg-muted"
			aria-hidden="true"
		/>
	{/if}
{/snippet}

<!-- The rows of the menu, for the dropdown's list and the sheet's alike. -->
{#snippet body(big: boolean)}
	{#if q.isLoading}
		<li role="none" class="text-fg-muted {big ? 'px-5 py-5 text-sm' : 'px-3 py-2 text-xs'}">
			{i18n.common_resolving()}
		</li>
	{:else if q.isError}
		<li
			role="none"
			title={q.error?.message}
			class="flex flex-col gap-1.5 text-status-failed {big
				? 'px-5 py-5 text-sm'
				: 'px-3 py-2 text-xs'}"
		>
			<span role="alert" class="flex items-start gap-2">
				<TriangleAlert
					class="mt-px h-3.5 w-3.5 shrink-0"
					aria-hidden="true"
				/>
				{i18n.playback_load_failed()}
			</span>
			<button
				bind:this={items[0]}
				type="button"
				role="menuitem"
				onclick={() => q.refetch()}
				onkeydown={(e) => onItemKey(e, 0)}
				class="self-start rounded font-medium underline-offset-2 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
			>
				{i18n.common_retry()}
			</button>
		</li>
	{:else if links.length === 0}
		<li role="none" class="grid place-items-center gap-1 {big ? 'px-5 py-9' : 'px-3 py-3'}">
			<ServerOff
				class="{big ? 'h-6 w-6' : 'h-5 w-5'} text-fg-faint"
				aria-hidden="true"
			/>
			<p class="text-fg-muted {big ? 'text-sm' : 'text-xs'}">
				{i18n.mediaserver_none()}
			</p>
			{#if auth.isAdmin}
				<a
					bind:this={items[0]}
					href="/settings/media-servers"
					role="menuitem"
					onkeydown={(e) => onItemKey(e, 0)}
					class="rounded text-accent hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {big
						? 'text-sm'
						: 'text-xs'}"
				>
					{i18n.playon_configure_servers()}
				</a>
			{/if}
		</li>
	{:else}
		{#each links as l, i (l.name)}
			{@const rowClass = twMerge(
				"flex w-full items-center justify-between text-left focus:outline-none focus-visible:bg-bg-hover",
				big
					? "gap-2 px-5 py-4 text-[15px]"
					: "min-h-11 gap-4 px-3 py-1.5 text-sm transition-colors lg:min-h-9",
			)}
			<li role="none" class={big ? "border-b border-border last:border-0" : "max-w-72"}>
				{#if l.url}
					<a
						bind:this={items[i]}
						href={l.url}
						target="_blank"
						rel="noopener noreferrer"
						role="menuitem"
						onclick={close}
						onkeydown={(e) => onItemKey(e, i)}
						class={twMerge(rowClass, big ? "active:bg-bg-hover" : "hover:bg-bg-hover")}
					>
						{@render row(l, big)}
					</a>
				{:else}
					<div
						bind:this={items[i]}
						role="menuitem"
						aria-disabled="true"
						tabindex="-1"
						onkeydown={(e) => onItemKey(e, i)}
						class={twMerge(rowClass, "opacity-50")}
					>
						{@render row(l, big)}
					</div>
				{/if}
			</li>
		{/each}
	{/if}
{/snippet}

<div class={primary ? "flex min-w-0 flex-1" : undefined}>
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
			aria-haspopup="dialog"
			aria-expanded={open}
			aria-label={i18n.action_play_on()}
			{disabled}
			title={disabled ? disabledTitle : i18n.action_play_on()}
			onclick={toggle}
			class="grid h-11 w-11 place-items-center rounded-lg border border-border-strong bg-bg-elevated text-fg transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:border-border disabled:text-fg-faint"
		>
			<Play class="h-[18px] w-[18px] text-accent" aria-hidden="true" />
		</button>
	{:else}
		<!-- Labelled from 512px of row; the row it sits in is the @container. -->
		<button
			bind:this={triggerEl}
			type="button"
			aria-haspopup="menu"
			aria-expanded={open}
			aria-label={i18n.action_play_on()}
			{disabled}
			title={disabled ? disabledTitle : i18n.action_play_on()}
			onclick={toggle}
			class="inline-flex h-10 w-11 items-center justify-center gap-2 whitespace-nowrap rounded-md border border-border-strong bg-bg-elevated text-sm font-medium max-lg:min-h-11 lg:w-10 @min-[512px]:w-auto @min-[512px]:px-4 text-fg hover:border-accent/60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:border-border disabled:text-fg-faint disabled:hover:border-border"
		>
			<span class="inline-flex items-center gap-2">
				<Play class="h-4 w-4 text-accent" aria-hidden="true" />
				<span class="hidden @min-[512px]:inline">{i18n.action_play_on()}</span>
			</span>
			<ChevronDown
				class={twMerge(
					"hidden h-3.5 w-3.5 text-fg-muted transition @min-[512px]:block",
					open && "rotate-180",
				)}
				aria-hidden="true"
			/>
		</button>
	{/if}
</div>

{#if !sheet}
	<DropdownMenu
		{open}
		anchor={triggerEl}
		onClose={() => (open = false)}
		minWidth="200px"
		role="menu"
		ariaLabel={i18n.action_play_on()}
	>
		{@render body(false)}
	</DropdownMenu>
{/if}

<!-- Phone: the same links as a bottom sheet, like every other list a phone opens
     in this app; a popover anchored to either trigger would run off a 390px
     viewport. -->
{#if open && sheet}
	<div use:portal>
		<BottomSheet
			title={i18n.action_play_on()}
			label={i18n.action_play_on()}
			onClose={close}
			hideFrom="md"
			class="max-h-[80dvh]"
			headerClass="pb-3"
			bodyClass="border-t border-border px-0 pb-[max(env(safe-area-inset-bottom),8px)]"
		>
			<ul aria-live="polite">
				{@render body(true)}
			</ul>
		</BottomSheet>
	</div>
{/if}
