<script lang="ts">
	import { Bookmark, ChevronRight, Search, UserRound } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Poster from "@components/shared/Poster.svelte";
	import ReleaseTile from "./ReleaseTile.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import { artistTally, releasesCount, type Artist } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// An artist and every release beside it. From md the strip shows as many
	// releases as the row has room for and the last slot turns into "+N", so a
	// row never scrolls sideways under a mouse. Below md it is a swipe strip.
	let {
		artist: a,
		canEdit = false,
		onSearch,
		onMonitor,
	}: {
		artist: Artist;
		canEdit?: boolean;
		onSearch?: () => void;
		onMonitor?: () => void;
	} = $props();

	let t = $derived(artistTally(a));
	let href = $derived(`/music/${a.id}`);
	const releaseHref = (id: number) => `/music/${a.id}?release=${id}`;
	let monitored = $derived(a.monitor !== "none");

	let stripW = $state(0);
	// Measured with a guarded observer rather than bind:clientWidth: the strip's
	// contents are derived from its width, so a write is only made when the
	// width really changed.
	function measure(node: HTMLElement) {
		let frame = 0;
		const ro = new ResizeObserver(() => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				const w = Math.round(node.clientWidth);
				if (w !== stripW) stripW = w;
			});
		});
		ro.observe(node);
		return {
			destroy() {
				cancelAnimationFrame(frame);
				ro.disconnect();
			},
		};
	}
	let wide = $derived(stripW >= 640);
	let tileW = $derived(wide ? 104 : 96);
	let gap = $derived(wide ? 12 : 10);
	let slots = $derived(Math.max(1, Math.floor((stripW + gap) / (tileW + gap))));
	let shown = $derived(a.releases.length > slots ? a.releases.slice(0, slots - 1) : a.releases);
	let hidden = $derived(a.releases.length - shown.length);

	let pctHave = $derived(t.released ? (t.have / t.released) * 100 : 0);
	let pctDl = $derived(t.released ? (t.downloading / t.released) * 100 : 0);
</script>

{#snippet avatar(size: string)}
	<div class={cn("relative shrink-0 overflow-hidden rounded-full bg-bg-card", size)}>
		<div class="absolute inset-0 grid place-items-center text-fg-faint">
			<UserRound class="h-1/2 w-1/2" aria-hidden="true" />
		</div>
		<Poster src={a.photo_url} alt="" class="relative h-full w-full object-cover" />
	</div>
{/snippet}

{#snippet actions(variant: "row" | "tile")}
	<LibraryActions
		base={`/music/artists/${a.id}`}
		title={a.name}
		media="music"
		queryKey="music"
		backHref="/music"
		hasFiles={a.size > 0}
		profile={a.quality_profile}
		searchLabel={t.wanted > 0 ? i18n.music_search_missing() : undefined}
		{onSearch}
		removeBody={i18n.music_remove_body()}
		filesLabel={i18n.music_delete_files_label()}
		{variant}
	/>
{/snippet}

{#snippet tally()}
	<span class="text-fg">{t.have}</span> / {releasesCount(t.released)}{#if t.wanted}
		<span class="text-fg-faint"> · </span><span class="text-status-wanted">{i18n.music_n_wanted({ count: String(t.wanted) })}</span>
	{/if}
{/snippet}

<div class="border-b border-border">
	<div class="py-4 md:hidden">
		<div class="flex items-center gap-1 pl-4 pr-2">
			<a {href} class="flex min-w-0 flex-1 items-center gap-3">
				{@render avatar("h-10 w-10")}
				<div class="min-w-0 flex-1">
					<p class="truncate text-[15px] font-semibold text-fg">{a.name}</p>
					<p class="truncate font-mono text-[11px] text-fg-muted">{@render tally()}</p>
				</div>
				{#if !canEdit}<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />{/if}
			</a>
			{#if canEdit}{@render actions("row")}{/if}
		</div>
		<div
			class="mt-3 flex gap-2.5 overflow-x-auto px-4 pb-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
		>
			{#each a.releases as r (r.id)}
				<ReleaseTile release={r} href={releaseHref(r.id)} width={92} />
			{/each}
		</div>
	</div>

	<div class="hidden gap-5 px-6 py-5 transition-colors hover:bg-white/[0.025] md:flex lg:gap-6">
		<div class="w-[168px] shrink-0 lg:w-[196px]">
			<a {href} class="group/name flex items-center gap-3 rounded-md focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring">
				{@render avatar("h-12 w-12")}
				<div class="min-w-0 flex-1">
					<p class="truncate text-[15px] font-semibold text-fg transition group-hover/name:text-accent-text">{a.name}</p>
					<p class="truncate text-[12px] text-fg-subtle">{a.genre}</p>
				</div>
			</a>
			<p class="mt-4 font-mono text-[11px] text-fg-muted">{@render tally()}</p>
			<div class="mt-1.5 flex h-[3px] w-full overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
				<div class="bg-status-available" style:width="{pctHave}%"></div>
				<div class="bg-status-downloading" style:width="{pctDl}%"></div>
			</div>
			{#if canEdit}
				<div class="mt-3 flex gap-1.5">
					{#if t.wanted > 0}
						<button
							type="button"
							onclick={onSearch}
							aria-label={i18n.music_search_missing_for({ name: a.name })}
							title={i18n.music_search_missing()}
							class="grid h-11 w-11 place-items-center rounded-md border border-border bg-bg-elevated text-fg-muted transition hover:text-fg lg:h-7 lg:w-7"
						>
							<Search size={14} aria-hidden="true" />
						</button>
					{/if}
					<button
						type="button"
						onclick={onMonitor}
						aria-pressed={monitored}
						aria-label={monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						title={monitored ? i18n.action_stop_monitoring() : i18n.action_monitor()}
						class={cn(
							"grid h-11 w-11 place-items-center rounded-md border border-border bg-bg-elevated transition lg:h-7 lg:w-7",
							monitored ? "text-accent-text" : "text-fg-subtle hover:text-fg",
						)}
					>
						<Bookmark size={14} fill={monitored ? "currentColor" : "none"} aria-hidden="true" />
					</button>
					{@render actions("tile")}
				</div>
			{/if}
		</div>
		<div class="flex min-w-0 flex-1 overflow-hidden" style:gap="{gap}px" use:measure>
			{#each shown as r (r.id)}
				<ReleaseTile release={r} href={releaseHref(r.id)} width={tileW} />
			{/each}
			{#if hidden > 0}
				<a
					{href}
					class="block shrink-0 rounded-md focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
					style:width="{tileW}px"
					aria-label={i18n.music_more_releases({ count: String(hidden), name: a.name })}
				>
					<div class="grid aspect-square place-items-center rounded-md border border-border bg-surface transition hover:border-border-strong hover:bg-surface-2">
						<div class="text-center">
							<p class="font-mono text-lg font-semibold text-fg">+{hidden}</p>
							<p class="text-[11px] text-fg-subtle">{i18n.music_more()}</p>
						</div>
					</div>
				</a>
			{/if}
		</div>
	</div>
</div>
