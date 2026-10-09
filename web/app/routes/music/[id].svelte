<script lang="ts">
	import { onMount } from "svelte";
	import { createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { activeRoute } from "@roxi/routify";
	import { Bookmark, Radar, Search, UserRound } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import { formatRelative } from "@lib/dates";
	import MediaHero from "@components/shared/MediaHero.svelte";
	import MonitorSelect from "@components/shared/MonitorSelect.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import Poster from "@components/shared/Poster.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import PeopleGrid, { type PersonTile } from "@components/shared/PeopleGrid.svelte";
	import InfoCard, { type InfoRow } from "@components/shared/InfoCard.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import ReleaseList from "@components/music/ReleaseList.svelte";
	import ReleasePanel from "@components/music/ReleasePanel.svelte";
	import ReleaseAccordion from "@components/music/ReleaseAccordion.svelte";
	import ReleaseSearchModal from "@components/music/ReleaseSearchModal.svelte";
	import {
		artistPeople,
		artistTally,
		collaborators,
		creditRoleLabel,
		defaultRelease,
		memberYears,
		personHref,
		reachRole,
		releasesCount,
		tracksCount,
		type Artist,
		type ArtistMonitor,
		type Member,
		type Reach,
		type Release,
		type Track,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let id = $state("");
	let selectedId = $state<number | null>(null);
	// Phone accordion: null follows the selection, -1 is "all closed".
	let openId = $state<number | null>(null);
	let tab = $state<string>("overview");
	// Manual search opens on the whole discography ("artist") or on a release id.
	let manualOpen = $state(false);
	let manualScope = $state("artist");
	function openManual(scope: string) {
		manualScope = scope;
		manualOpen = true;
	}

	// The release a tile linked to rides in ?release=, read off the route being
	// rendered (window.location still names the outgoing page at mount).
	onMount(() =>
		activeRoute.subscribe((r) => {
			if (!r) return;
			const [path = "", search] = (r.url ?? "").split("?");
			const next = /^\/music\/(\d+)$/.exec(path)?.[1];
			if (!next || next === id) return;
			id = next;
			// The route component is reused from artist to artist.
			manualOpen = false;
			const rel = Number(new URLSearchParams(search ?? "").get("release")) || null;
			selectedId = rel;
			openId = rel;
			// A tile that named a release came for its tracks; a bare artist link
			// opens on the overview, as a series does.
			tab = rel ? "discography" : "overview";
		}),
	);

	const artistQuery = createQuery<Artist>(() => ({
		queryKey: ["music", "artist", id],
		queryFn: () => api<Artist>(`/music/artists/${id}`),
		enabled: !!id,
	}));
	let artist = $derived(artistQuery.data);
	let tally = $derived(artist ? artistTally(artist) : null);
	let selected = $derived(
		artist ? (artist.releases.find((r) => r.id === selectedId) ?? defaultRelease(artist.releases)) : undefined,
	);
	let open = $derived(openId === null ? selected?.id : openId);
	let canEdit = $derived(auth.canAddDirectly);

	function select(r: Release) {
		selectedId = r.id;
		if (typeof window !== "undefined" && window.location.pathname === `/music/${id}`)
			window.history.replaceState(null, "", `/music/${id}?release=${r.id}`);
	}

	const MONITOR: { key: ArtistMonitor; label: string }[] = [
		{ key: "all", label: i18n.music_monitor_all() },
		{ key: "future", label: i18n.music_monitor_future() },
		{ key: "manual", label: i18n.music_monitor_manual() },
		{ key: "none", label: i18n.music_monitor_none() },
	];

	const qc = useQueryClient();
	const invalidate = () => qc.invalidateQueries({ queryKey: ["music"] });
	async function setMonitor(v: string) {
		await api(`/music/artists/${id}`, { method: "PATCH", body: { monitor: v } });
		invalidate();
	}
	let lastMonitor: ArtistMonitor = "all";
	function toggleMonitor() {
		if (!artist) return;
		if (artist.monitor === "none") setMonitor(lastMonitor);
		else {
			lastMonitor = artist.monitor;
			setMonitor("none");
		}
	}
	async function toggleRelease(r: Release) {
		await api(`/music/releases/${r.id}`, { method: "PATCH", body: { monitored: !r.monitored } });
		invalidate();
	}
	async function searchMissing() {
		if (!artist) return;
		await api(`/music/artists/${id}/search`, { method: "POST" });
		toast.ok(i18n.music_search_started({ title: artist.name }));
	}
	async function searchRelease(r: Release) {
		await api(`/music/releases/${r.id}/search`, { method: "POST" });
		toast.ok(i18n.music_search_started({ title: r.title }));
	}
	async function searchTrack(t: Track) {
		await api(`/music/tracks/${t.id}/search`, { method: "POST" });
		toast.ok(i18n.music_search_started({ title: t.title }));
	}
	let deletingTrack = $state<Track | null>(null);
	let deletingPending = $state(false);
	async function deleteTrack() {
		const t = deletingTrack;
		if (!t) return;
		deletingPending = true;
		try {
			await api(`/music/tracks/${t.id}/file`, { method: "DELETE" });
			deletingTrack = null;
			invalidate();
			toast.ok(i18n.file_deleted());
		} catch (e) {
			toast.err(errorText(e, i18n.common_delete_failed()));
		} finally {
			deletingPending = false;
		}
	}

	let meta = $derived.by(() => {
		if (!artist || !tally) return [];
		const p: string[] = [releasesCount(tally.released)];
		if (tally.upcoming) p.push(i18n.music_n_upcoming({ count: String(tally.upcoming) }));
		p.push(tracksCount(artist.track_count));
		if (artist.size) p.push(formatBytes(artist.size, ""));
		return p;
	});
	const caps = "font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted";

	// The cast and crew of a discography. The overview shows the line-up and
	// the few names that recur most; Credits shows everyone, by what they did.
	let people = $derived(artist ? artistPeople(artist) : null);
	let collabs = $derived(people ? collaborators(people, 6) : []);
	let moreCredits = $derived(!!people && people.total > people.current.length + collabs.length);
	const memberTile = (m: Member): PersonTile => ({
		key: m.name,
		name: m.name,
		photo_url: m.photo_url,
		href: personHref(m),
		role: m.instruments.join(", "),
		note: memberYears(m),
		dim: !!m.to,
	});
	const reachTile = (x: Reach, role: string): PersonTile => ({
		key: x.person.name,
		name: x.person.name,
		photo_url: x.person.photo_url,
		href: personHref(x.person),
		role,
		note: releasesCount(x.releases),
	});
	let creditSections = $derived(
		people
			? [
					{ key: "members", title: i18n.lookup_members(), people: people.current.map(memberTile) },
					{ key: "former", title: i18n.music_members_former(), people: people.former.map(memberTile) },
					{ key: "featured", title: i18n.music_featured_artists(), people: people.featured.map((x) => reachTile(x, tracksCount(x.tracks))) },
					{ key: "musicians", title: i18n.music_musicians(), people: people.musicians.map((x) => reachTile(x, x.instruments.join(", "))) },
					{ key: "production", title: i18n.music_production(), people: people.production.map((x) => reachTile(x, x.roles.map(creditRoleLabel).join(" · "))) },
				].filter((s) => s.people.length > 0)
			: [],
	);

	let artistRows = $derived.by<InfoRow[]>(() => {
		if (!artist) return [];
		const labels = [...new Set(artist.releases.map((r) => r.label).filter((l): l is string => !!l))];
		const rows: InfoRow[] = [];
		if (artist.type) rows.push({ label: i18n.common_type(), value: artist.type === "group" ? i18n.lookup_group() : i18n.common_person(), mono: false });
		rows.push({ label: i18n.music_fact_genre(), value: artist.genre, mono: false });
		if (artist.origin) rows.push({ label: i18n.music_fact_from(), value: artist.origin, mono: false });
		if (artist.since) rows.push({ label: i18n.music_fact_since(), value: String(artist.since) });
		if (labels.length) rows.push({ label: labels.length === 1 ? i18n.music_fact_label() : i18n.music_fact_labels(), value: labels.join(", "), mono: false });
		if (artist.mbid)
			rows.push({ label: "MusicBrainz", value: [{ text: artist.mbid.slice(0, 8), href: `https://musicbrainz.org/artist/${artist.mbid}`, external: true }], mono: true });
		return rows;
	});
	let libraryRows = $derived.by<InfoRow[]>(() => {
		if (!artist || !tally) return [];
		return [
			{ label: i18n.quality_profile(), value: artist.quality_profile || i18n.quality_server_default() },
			{ label: i18n.action_monitor(), value: MONITOR.find((o) => o.key === artist.monitor)?.label ?? "", mono: false },
			{ label: i18n.music_fact_releases(), value: `${tally.have} / ${tally.released}` },
			{ label: i18n.music_fact_on_disk(), value: formatBytes(artist.size, "") },
			{ label: i18n.music_fact_last_added(), value: formatRelative(artist.last_added_at) },
		];
	});
	const h3 = "font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";
</script>

{#if artistQuery.isLoading || !id}
	<span class="sr-only" role="status">{i18n.common_loading()}</span>
	<div class="px-4 pt-6 md:px-8">
		<Skeleton w="80px" h={28} />
		<div class="mt-8 flex flex-col items-center gap-6 md:flex-row md:items-end">
			<div class="h-44 w-44 animate-pulse rounded-full bg-white/[0.06] md:h-[180px] md:w-[180px]"></div>
			<div class="flex w-full flex-col gap-3">
				<Skeleton w="60%" h={36} />
				<Skeleton w="40%" h={14} />
			</div>
		</div>
	</div>
{:else if artistQuery.isError || !artist || !tally}
	<div class="px-4 pt-6 md:px-8">
		<div class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center">
			<p class="text-sm font-semibold text-status-failed">{i18n.music_load_artist_failed()}</p>
			<p class="mt-1 text-xs text-fg-subtle">{errorText(artistQuery.error, i18n.common_unknown_error())}</p>
		</div>
	</div>
{:else}
	<div class={cn(canEdit && "pb-24 md:pb-0")}>
		<MediaHero
			backdrop={artist.photo_url}
			backHref="/music"
			backLabel={i18n.music_label()}
			cols="md:grid-cols-[180px_1fr] lg:grid-cols-[220px_1fr]"
			artWrap="w-44"
			labelId="artist-title"
			title={artist.name}
			meta={meta}
			overview={artist.overview}
		>
			{#snippet art()}
				<div class="relative aspect-square w-full overflow-hidden rounded-full bg-bg-card shadow-[0_24px_48px_rgb(0_0_0_/0.5)]">
					<div class="absolute inset-0 grid place-items-center text-fg-faint">
						<UserRound class="h-1/3 w-1/3" aria-hidden="true" />
					</div>
					<Poster src={artist.photo_url} alt="" loading="eager" class="relative h-full w-full object-cover" />
				</div>
			{/snippet}
			{#snippet pills()}
				<StatusPill status={artist.status} size="md" variant="translucent" live={artist.status === "downloading"} />
				<span class={caps}>{artist.genre}</span>
				{#if artist.origin}
					<span class="text-fg-faint" aria-hidden="true">·</span>
					<span class={caps}>{artist.origin}</span>
				{/if}
				{#if artist.since}
					<span class="text-fg-faint" aria-hidden="true">·</span>
					<span class={caps}>{i18n.music_since({ year: String(artist.since) })}</span>
				{/if}
			{/snippet}
			{#snippet extra()}
				{#if tally.released > 0}
					<div class="mt-5 max-w-[560px]">
						<div class="mb-2 font-mono text-xs text-fg-muted">
							<span class="text-fg">{tally.have}</span> / {tally.released}
							<span class="text-fg-subtle">{i18n.music_releases_complete()}</span>
							{#if tally.downloading}
								<span class="text-fg-faint"> · </span><span class="text-status-downloading">{i18n.music_n_downloading({ count: String(tally.downloading) })}</span>
							{/if}
							{#if tally.wanted}
								<span class="text-fg-faint"> · </span><span class="text-status-wanted">{i18n.music_n_wanted({ count: String(tally.wanted) })}</span>
							{/if}
						</div>
						<div class="flex h-[3px] w-full overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
							<div class="bg-status-available" style:width="{(tally.have / tally.released) * 100}%"></div>
							<div class="bg-status-downloading" style:width="{(tally.downloading / tally.released) * 100}%"></div>
						</div>
					</div>
				{/if}
			{/snippet}
			{#snippet actions()}
				{#if canEdit}
					<!-- Two lines from md, as on the series page: what the artist follows
					     (label, select, bookmark), then the searches and the menu.
					     The automatic search (Search missing, or Search now with nothing
					     wanted) and Manual search, which lists results to pick from for
					     the discography or one release. One flow in DOM order: a phone
					     keeps label over select + bookmark + menu, with the searches in
					     the pinned bar. Manual search is an icon below 512px of row: in
					     French the pair and the menu are 476 and the tablet row is 470,
					     so the menu would wrap onto a third line. -->
					<div class="@container">
					<div class="flex flex-wrap items-center gap-2 md:gap-x-2.5 md:gap-y-3">
						<span class="basis-full font-mono text-[10.5px] uppercase tracking-[0.1em] text-fg-subtle md:basis-auto">{i18n.action_monitor()}</span>
						<MonitorSelect
							value={artist.monitor}
							options={MONITOR}
							onChange={setMonitor}
							label={i18n.action_monitor()}
							class="flex-1 md:flex-none"
						/>
						<button
							type="button"
							onclick={toggleMonitor}
							aria-pressed={artist.monitor !== "none"}
							aria-label={artist.monitor !== "none" ? i18n.action_stop_monitoring() : i18n.action_monitor()}
							title={artist.monitor !== "none" ? i18n.action_stop_monitoring() : i18n.action_monitor()}
							class={cn(
								"grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border bg-bg-elevated/80 transition hover:border-border-strong",
								artist.monitor !== "none" ? "text-accent-text" : "text-fg-subtle",
							)}
						>
							<Bookmark size={16} fill={artist.monitor !== "none" ? "currentColor" : "none"} aria-hidden="true" />
						</button>
						<span class="hidden h-0 basis-full md:block" aria-hidden="true"></span>
						<!-- Always offered, as the book page's Search now: with nothing
						     wanted it looks for upgrades to what the profile allows. -->
						<button
							type="button"
							onclick={searchMissing}
							disabled={artist.monitor === "none"}
							class="hidden h-11 items-center gap-2 rounded-lg bg-accent px-4 text-[14px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50 md:inline-flex"
						>
							<Radar size={16} aria-hidden="true" />
							{tally.wanted > 0 ? i18n.music_search_missing() : i18n.music_search_now()}
						</button>
						<button
							type="button"
							onclick={() => openManual("artist")}
							aria-label={i18n.action_manual_search()}
							title={i18n.action_manual_search()}
							class={cn(
								"hidden h-11 min-w-11 items-center justify-center gap-2 rounded-lg border border-border-strong bg-white/[0.08] text-[14px] font-medium text-fg backdrop-blur-sm transition hover:bg-white/[0.14] md:inline-flex",
								"px-3 @lg:px-4",
							)}
						>
							<Search size={16} aria-hidden="true" />
							<span class="hidden @lg:inline">{i18n.action_manual_search()}</span>
						</button>
						<LibraryActions
							base={`/music/artists/${artist.id}`}
							title={artist.name}
							media="music"
							queryKey="music"
							backHref="/music"
							hasFiles={artist.size > 0}
							profile={artist.quality_profile}
							removeBody={i18n.music_remove_body()}
							filesLabel={i18n.music_delete_files_label()}
						/>
					</div>
					</div>
				{/if}
			{/snippet}
		</MediaHero>

		<nav class="sticky top-16 z-10 border-b border-border bg-bg-deep/70 px-4 backdrop-blur-md saturate-150 md:px-8">
			<div class="flex w-full gap-0.5">
				{#each [{ key: "overview", label: i18n.common_overview(), n: null }, { key: "discography", label: i18n.music_discography(), n: artist.releases.length }, { key: "credits", label: i18n.music_credits(), n: people?.total || null }] as t (t.key)}
					{@const active = tab === t.key}
					<button
						type="button"
						onclick={() => (tab = t.key)}
						aria-current={active ? "page" : undefined}
						class={cn("relative -mb-px shrink-0 px-3 py-3.5 text-[13px] font-medium transition md:px-4", active ? "text-fg" : "text-fg-subtle hover:text-fg")}
					>
						<span>{t.label}</span>
						{#if t.n != null}<span class="ml-1.5 font-mono text-[11px] text-fg-faint">{t.n}</span>{/if}
						{#if active}<span aria-hidden="true" class="absolute inset-x-3 -bottom-px h-0.5 rounded-t-sm bg-accent"></span>{/if}
					</button>
				{/each}
			</div>
		</nav>

		{#if tab === "discography"}
			<div class="md:hidden">
				<ReleaseAccordion
					releases={artist.releases}
					openId={open}
					onToggle={(rid) => (openId = open === rid ? -1 : rid)}
					{canEdit}
					onSearch={searchRelease}
					onManualSearch={(r) => openManual(String(r.id))}
					onMonitor={toggleRelease}
					onSearchTrack={searchTrack}
					onDeleteTrack={(t) => (deletingTrack = t)}
				/>
			</div>
			<!-- 1fr, not minmax(0,1fr): the browser Tailwind build hangs the tab on
			     an arbitrary grid template that ends in `minmax(0,1fr)]`. The panel is
			     min-w-0, which gives the same floor. -->
			<div class="hidden gap-6 px-8 py-6 md:grid md:grid-cols-[200px_1fr] lg:grid-cols-[250px_1fr]">
				<ReleaseList releases={artist.releases} selectedId={selected?.id} onSelect={select} />
				{#if selected}
					<ReleasePanel
						release={selected}
						{canEdit}
						onSearch={() => searchRelease(selected)}
						onManualSearch={() => openManual(String(selected.id))}
						onMonitor={() => toggleRelease(selected)}
						onSearchTrack={searchTrack}
						onDeleteTrack={(t) => (deletingTrack = t)}
					/>
				{/if}
			</div>
		{:else if tab === "credits"}
			<div class="flex flex-col gap-8 px-4 py-6 md:px-8">
				{#each creditSections as s (s.key)}
					<section aria-labelledby="credits-{s.key}">
						<h2 id="credits-{s.key}" class="mb-3 flex items-baseline gap-2 {h3}">
							{s.title}<span class="tracking-normal">{s.people.length}</span>
						</h2>
						<PeopleGrid people={s.people} round />
					</section>
				{:else}
					<div class="rounded-lg border border-dashed border-border bg-bg-elevated/40 py-10 text-center">
						<p class="text-sm font-medium text-fg">{i18n.music_no_credits()}</p>
						<p class="mt-1 text-xs text-fg-muted">{i18n.music_no_credits_help()}</p>
					</div>
				{/each}
			</div>
		{:else}
			<!-- The movie and series overview: about and people on the left, the
			     facts beside them. -->
			<div class="grid gap-6 px-4 py-6 md:grid-cols-[1fr_260px] md:gap-7 md:px-8 lg:grid-cols-[1fr_320px] lg:gap-10">
				<section class="min-w-0" aria-labelledby="artist-about">
					<h2 id="artist-about" class={h3}>{i18n.lookup_about()}</h2>
					<p class="mt-3 max-w-[720px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">
						{artist.overview ?? i18n.music_no_overview()}
					</p>
					{#if people && people.current.length > 0}
						<div class="my-5 h-px bg-border"></div>
						{@render head(i18n.lookup_members(), moreCredits)}
						<PeopleGrid people={people.current.map(memberTile)} dense round />
					{/if}
					{#if collabs.length > 0}
						<div class="my-5 h-px bg-border"></div>
						{@render head(i18n.music_collaborators(), moreCredits && !people?.current.length)}
						<PeopleGrid people={collabs.map((x) => reachTile(x, reachRole(x)))} layout="list" round />
					{/if}
				</section>
				<aside class="flex min-w-0 flex-col gap-4">
					<InfoCard id="artist-info" title={i18n.music_artist()} rows={artistRows} />
					<InfoCard id="artist-library" title={i18n.nav_library()} rows={libraryRows} />
				</aside>
			</div>
		{/if}

		{#if canEdit}
			<!-- Phone: the searches, as on the book page — the automatic one, then
			     the manual one as an icon. -->
			<div
				class="fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+3.5rem)] z-30 flex items-center gap-2 border-t border-border bg-bg-elevated/95 px-3 pb-4 pt-2.5 backdrop-blur-md md:hidden"
			>
					<button
						type="button"
						onclick={searchMissing}
						disabled={artist.monitor === "none"}
						class="inline-flex h-11 min-w-0 flex-1 items-center justify-center gap-2 rounded-lg bg-accent text-[14px] font-semibold text-fg-on-accent disabled:opacity-50"
					>
						<Radar size={16} aria-hidden="true" />
						{tally.wanted > 0 ? i18n.music_search_missing() : i18n.music_search_now()}
					</button>
					<button
						type="button"
						onclick={() => openManual("artist")}
						aria-label={i18n.action_manual_search()}
						title={i18n.action_manual_search()}
						class="grid h-11 w-11 shrink-0 place-items-center rounded-lg border border-border-strong bg-bg-elevated text-fg-muted transition active:bg-surface"
					>
						<Search size={18} aria-hidden="true" />
					</button>
			</div>
		{/if}

		{#if canEdit}
			<ReleaseSearchModal open={manualOpen} {artist} initialScope={manualScope} onClose={() => (manualOpen = false)} />
		{/if}

		<Dialog
			open={!!deletingTrack}
			title={i18n.music_delete_track_title({ title: deletingTrack?.title ?? "" })}
			onClose={() => (deletingTrack = null)}
			actions={[
				{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
				{ label: i18n.action_delete_file(), variant: "danger", dismiss: false, pending: deletingPending, onClick: deleteTrack },
			]}
		>
			<p class="text-sm leading-relaxed text-fg-muted [text-wrap:pretty]">{i18n.music_delete_track_body()}</p>
		</Dialog>
	</div>
{/if}

{#snippet head(title: string, more: boolean)}
	<div class="mb-3 flex items-baseline justify-between gap-3">
		<h3 class={h3}>{title}</h3>
		{#if more}
			<button type="button" onclick={() => (tab = "credits")} class="touch-hit font-mono text-[11px] text-accent-text transition hover:text-accent">
				{i18n.common_view_all()}
			</button>
		{/if}
	</div>
{/snippet}
