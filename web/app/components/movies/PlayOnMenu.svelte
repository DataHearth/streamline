<script lang="ts">
	import { Play, ChevronDown, ExternalLink, ServerOff } from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { api } from "../../lib/api";
	import BrandLogo from "../settings/BrandLogo.svelte";
	import type { PlayOnLink, PlayOnLinkList } from "../../lib/types";

	type Props = {
		// Play-on endpoint (movie or series) returning a PlayOnLinkList.
		path: string;
		queryKey: readonly unknown[];
		disabled?: boolean;
		disabledTitle?: string;
	};
	let {
		path,
		queryKey,
		disabled = false,
		disabledTitle,
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
</script>

{#snippet row(l: PlayOnLink)}
	<span class="flex min-w-0 items-center gap-2.5">
		<BrandLogo name={l.server_type} size={18} />
		<span class="flex min-w-0 flex-col">
			<span class="truncate font-medium text-fg">{l.name}</span>
			{#if l.status === "fallback"}
				<span class="text-[10px] text-fg-muted">↗ Library</span>
			{:else if l.status === "unavailable"}
				<span class="text-[10px] text-fg-faint">Unavailable</span>
			{/if}
		</span>
	</span>
	{#if l.status !== "unavailable"}
		<ExternalLink class="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
	{/if}
{/snippet}

<div class="relative" onkeydown={onKey} role="presentation">
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
			Play on
		</span>
		<ChevronDown
			class="h-4 w-4 text-fg-muted transition {open ? 'rotate-180' : ''}"
			aria-hidden="true"
		/>
	</button>

	{#if open}
		<div
			role="menu"
			aria-live="polite"
			transition:fly={{ duration: 140, y: -4, easing: cubicOut }}
			class="absolute right-0 z-30 mt-1 w-[260px] overflow-hidden rounded-md border border-border bg-bg-elevated shadow-3"
		>
			{#if q.isLoading}
				<p class="px-3 py-3 text-xs text-fg-muted">Resolving…</p>
			{:else if links.length === 0}
				<div class="grid place-items-center gap-1 px-3 py-4">
					<ServerOff
						class="h-5 w-5 text-fg-faint"
						aria-hidden="true"
					/>
					<p class="text-xs text-fg-muted">
						No media servers configured.
					</p>
					<a
						href="/settings/media-servers"
						class="text-xs text-accent hover:underline"
					>
						Configure servers →
					</a>
				</div>
			{:else}
				<ul>
					{#each links as l (l.name)}
						<li>
							{#if l.url}
								<a
									href={l.url}
									target="_blank"
									rel="noopener noreferrer"
									role="menuitem"
									onclick={close}
									class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm hover:bg-bg-hover"
								>
									{@render row(l)}
								</a>
							{:else}
								<div
									role="menuitem"
									aria-disabled="true"
									class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm opacity-50"
								>
									{@render row(l)}
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
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
