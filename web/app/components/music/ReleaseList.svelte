<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { slide } from "svelte/transition";
	import { cubicInOut, cubicOut } from "svelte/easing";
	import { api } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { cn } from "@lib/cn";
	import MonitorToggle from "@components/shared/MonitorToggle.svelte";
	import ReleaseCover from "./ReleaseCover.svelte";
	import ReleaseMark from "./ReleaseMark.svelte";
	import { releaseGroups, releaseLine, releaseTypeLabel, type Release, type ReleaseGroup } from "@lib/music-books";
	import { albumPosterUrl } from "@lib/posters";
	import type { LibraryConfig } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The artist page's left column from md: releases grouped the way seasons
	// are, the selected one opening on the right.
	let {
		releases,
		selectedId,
		onSelect,
		onMonitorGroup,
		monitorPending = false,
	}: {
		releases: Release[];
		selectedId: number | undefined;
		onSelect: (r: Release) => void;
		onMonitorGroup?: (releases: Release[], monitored: boolean) => void;
		monitorPending?: boolean;
	} = $props();

	let groups = $derived(releaseGroups(releases));

	// /config/library is admin-only. Anyone else opens a section on what is
	// monitored or held in it, which the release rows already say.
	const cfg = createQuery<LibraryConfig>(() => ({
		queryKey: ["config", "library"],
		queryFn: () => api<LibraryConfig>("/config/library"),
		enabled: auth.isAdmin,
	}));
	let toggled = $state<Record<string, boolean>>({});
	const opensByDefault = (g: ReleaseGroup) =>
		g.types.some((t) => cfg.data?.music_monitored_types?.includes(t)) ||
		g.releases.some((r) => r.monitored || r.tracks_have > 0 || r.id === selectedId);
</script>

{#snippet item(r: Release)}
	{@const sel = r.id === selectedId}
	{@const up = r.status === "upcoming"}
	<li>
		<button
			type="button"
			onclick={() => onSelect(r)}
			aria-current={sel ? "true" : undefined}
			class={cn(
				"flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
				sel ? "bg-accent-soft ring-1 ring-accent-line" : "hover:bg-surface",
			)}
		>
			<div class="w-10 shrink-0">
				<ReleaseCover
					src={albumPosterUrl(r.id)}
					dim={up || (r.status === "wanted" && r.tracks_have === 0)}
					dashed={up}
					class="rounded"
				/>
			</div>
			<div class="min-w-0 flex-1">
				<p class={cn("truncate text-[13px] font-medium", sel ? "text-accent-text" : "text-fg")}>{r.title}</p>
				<p class="truncate font-mono text-[10.5px] text-fg-subtle">
					{up ? releaseTypeLabel(r.type) : releaseLine(r)}
				</p>
			</div>
			<ReleaseMark release={r} />
		</button>
	</li>
{/snippet}

<nav aria-label={i18n.music_discography()} class="flex flex-col">
	{#each groups as g, i (g.key)}
		{@const open = toggled[g.key] ?? opensByDefault(g)}
		{@const allMonitored = g.releases.every((r) => r.monitored)}
		<div class={cn("flex items-center gap-1 pr-1", i > 0 && "mt-3")}>
			<button
				type="button"
				aria-expanded={open}
				onclick={() => (toggled[g.key] = !open)}
				class="flex min-h-11 min-w-0 flex-1 items-center gap-1.5 rounded-md px-2.5 text-left font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint transition hover:text-fg-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring lg:min-h-7"
			>
				<ChevronRight size={12} class={cn("shrink-0 transition-transform duration-150", open && "rotate-90")} aria-hidden="true" />
				<span class="truncate">{g.label} · {g.releases.length}</span>
			</button>
			{#if onMonitorGroup}
				<MonitorToggle
					variant="ghost"
					size="xs"
					monitored={allMonitored}
					pending={monitorPending}
					startLabel={i18n.music_group_monitor({ group: g.label })}
					stopLabel={i18n.music_group_unmonitor({ group: g.label })}
					onToggle={() => onMonitorGroup(g.releases, !allMonitored)}
				/>
			{/if}
		</div>
		{#if open}
			<!-- Opening eases in: a 2000px section under ease-out passes the list's
			     visible height in one frame, leaving its slow tail off screen. Only a
			     click animates, not the config answering during page load. -->
			<ul
				class="flex flex-col gap-0.5"
				in:slide={{ duration: toggled[g.key] === undefined ? 0 : 280, easing: cubicInOut }}
				out:slide={{ duration: 180, easing: cubicOut }}
			>
				{#each g.releases as r (r.id)}
					{@render item(r)}
				{/each}
			</ul>
		{/if}
	{/each}
</nav>
