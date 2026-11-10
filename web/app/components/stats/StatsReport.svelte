<script lang="ts">
	import type { Snippet } from "svelte";
	import StatsCard from "./StatsCard.svelte";
	import StatsFigure from "./StatsFigure.svelte";
	import StatsMeter from "./StatsMeter.svelte";
	import StatsDaily from "./StatsDaily.svelte";
	import StatsFunnel from "./StatsFunnel.svelte";
	import StatsMediaTable from "./StatsMediaTable.svelte";
	import StatsVolumes from "./StatsVolumes.svelte";
	import StatsFailures from "./StatsFailures.svelte";
	import StatsIndexers from "./StatsIndexers.svelte";
	import StatsPeople from "./StatsPeople.svelte";
	import {
		KIND_TOKEN,
		RANGE_LABEL,
		delta,
		fmtCount,
		fmtSize,
		fmtWait,
		toMeter,
		totals,
		type Stats,
	} from "@lib/stats";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The stats page, read top to bottom, a section per question:
	// what is in the library, how well the pipeline fills it, who asks for
	// what. Each section opens with a sentence and its four numbers, then the
	// detail. Phone keeps the same order; nothing is hidden behind a control.
	let { stats, rangeControl }: { stats: Stats; rangeControl: Snippet } = $props();

	const UNIT = {
		movie: i18n.lc_movies,
		series: i18n.lc_episodes,
		music: i18n.stats_lc_albums,
		books: i18n.stats_lc_books,
	};

	let t = $derived(totals(stats));
	let prev = $derived(stats.previous);
	let lib = $derived(stats.library);
	let pipe = $derived(stats.pipeline);
	let users = $derived(stats.users);
	let range = $derived(RANGE_LABEL[stats.range]());

	let titles = $derived(lib.media.reduce((a, m) => a + m.titles, 0));
	let files = $derived(lib.media.reduce((a, m) => a + m.files, 0));
	let bytes = $derived(lib.media.reduce((a, m) => a + m.bytes, 0));
	let wanted = $derived(lib.media.reduce((a, m) => a + m.wanted, 0));
	let wantedLine = $derived(
		lib.media
			.filter((m) => m.wanted > 0)
			.map((m) => `${fmtCount(m.wanted)} ${UNIT[m.kind]()}`)
			.join(" · "),
	);
	let videoFiles = $derived(lib.resolution.reduce((a, s) => a + s.count, 0));

	const sectionHead = "text-base font-semibold tracking-tight text-fg";
	const sectionLede = "mt-1 text-[13px] text-fg-subtle";
</script>

<div class="flex flex-col gap-10 md:gap-12">
	<!-- Library -->
	<section aria-labelledby="stats-library" class="flex flex-col gap-4">
		<header>
			<div class="flex items-baseline gap-2">
				<h2 id="stats-library" class={sectionHead}>{i18n.nav_library()}</h2>
				<span class="font-mono text-[11px] text-fg-subtle">{i18n.stats_library_now()}</span>
			</div>
			<p class={sectionLede}>
				{i18n.stats_library_summary({ titles: fmtCount(titles), count: lib.media.length, size: fmtSize(bytes) })}
			</p>
		</header>
		<div class="grid grid-cols-2 gap-x-4 gap-y-5 md:grid-cols-4">
			<StatsFigure size="lg" label={i18n.stats_titles()} value={fmtCount(titles)} />
			<StatsFigure size="lg" label={i18n.common_files()} value={fmtCount(files)} />
			<StatsFigure size="lg" label={i18n.stats_on_disk()} value={fmtSize(bytes)} />
			<StatsFigure size="lg" label={i18n.status_wanted()} value={fmtCount(wanted)} sub={wantedLine} tone={wanted > 0 ? "wanted" : ""} />
		</div>

		<div class="grid grid-cols-1 gap-4 lg:grid-cols-[1.5fr_1fr]">
			<StatsCard title={i18n.stats_by_medium()} flush>
				<StatsMediaTable media={lib.media} />
			</StatsCard>
			<StatsCard title={i18n.stats_storage()}>
				<StatsVolumes volumes={lib.volumes} />
			</StatsCard>
		</div>

		<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
			<StatsCard
				title={i18n.stats_resolution()}
				caption={i18n.stats_n_video_files({ count: fmtCount(videoFiles) })}
			>
				<StatsMeter label={i18n.stats_resolution()} slices={toMeter(lib.resolution, "indigo")} />
			</StatsCard>
			<StatsCard title={i18n.stats_video_codec()}>
				<StatsMeter label={i18n.stats_video_codec()} slices={toMeter(lib.video_codec, "teal")} />
			</StatsCard>
			<StatsCard title={i18n.stats_dynamic_range()}>
				<StatsMeter label={i18n.stats_dynamic_range()} slices={toMeter(lib.hdr, "gold")} />
			</StatsCard>
			<StatsCard title={i18n.stats_audio()}>
				<StatsMeter label={i18n.stats_audio()} slices={toMeter(lib.audio, "rose")} />
			</StatsCard>
		</div>
		{#if lib.unprobed > 0}
			<p class="-mt-1 font-mono text-[11.5px] text-fg-subtle">
				{i18n.stats_unprobed({ count: fmtCount(lib.unprobed) })}
				<a href="/settings/media-probe" class="touch-hit text-accent-text hover:underline">{i18n.stats_probe_settings()}</a>
			</p>
		{/if}

		<StatsCard title={i18n.stats_largest()} flush>
			<ul>
				{#each lib.largest as f, i (f.href)}
					<li class={i > 0 ? "border-t border-border" : ""}>
						<a href={f.href} class="flex min-w-0 items-center gap-3 px-4 py-2.5 transition hover:bg-surface md:px-5">
							<span class="h-2 w-2 shrink-0 rounded-full" style:background="var(--kind-{KIND_TOKEN[f.kind]})" aria-hidden="true"></span>
							<span class="min-w-0 flex-1 truncate text-[13.5px] text-fg">{f.title}</span>
							<span class="hidden shrink-0 font-mono text-[11.5px] text-fg-subtle sm:inline">{f.quality}</span>
							<span class="w-16 shrink-0 text-right font-mono text-[12.5px] tabular-nums text-fg">{fmtSize(f.bytes)}</span>
						</a>
					</li>
				{/each}
			</ul>
		</StatsCard>
	</section>

	<!-- The period bounds these two sections and nothing above them, so it sits
	     here and stays in view while they scroll. -->
	<div class="flex flex-col gap-10 md:gap-12">
		<div
			class="sticky top-(--top-bar-height) z-20 -mx-4 -mb-6 flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border bg-bg-deep px-4 py-2.5 md:-mx-8 md:-mb-8 md:px-8"
		>
			{@render rangeControl()}
		</div>

		<!-- Pipeline -->
		<section aria-labelledby="stats-pipeline" class="flex flex-col gap-4">
			<header>
				<h2 id="stats-pipeline" class={sectionHead}>{i18n.stats_section_pipeline()}</h2>
				<p class={sectionLede}>
					{i18n.stats_pipeline_summary({ imported: fmtCount(t.imported), grabbed: fmtCount(t.grabbed), range })}
				</p>
			</header>
			<div class="grid grid-cols-2 gap-x-4 gap-y-5 md:grid-cols-4">
				<StatsFigure size="lg" label={i18n.stats_grabbed()} value={fmtCount(t.grabbed)} delta={delta(t.grabbed, prev.grabbed)} />
				<StatsFigure size="lg" label={i18n.stats_imported()} value={fmtCount(t.imported)} delta={delta(t.imported, prev.imported)} />
				<StatsFigure
					size="lg"
					label={i18n.stats_failed()}
					value={fmtCount(t.failed)}
					delta={delta(t.failed, prev.failed, "bad")}
					tone={t.failed > 0 ? "failed" : ""}
				/>
				<StatsFigure
					size="lg"
					label={i18n.stats_grab_to_import()}
					value={fmtWait(pipe.grab_to_import_p50_s)}
					sub={i18n.stats_median_p90({ p90: fmtWait(pipe.grab_to_import_p90_s) })}
				/>
			</div>

			<StatsCard title={i18n.stats_daily()} caption={range}>
				<StatsDaily days={pipe.daily} height={168} />
			</StatsCard>

			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				<StatsCard
					title={i18n.stats_funnel()}
					caption={pipe.held > 0 ? i18n.stats_n_held({ count: pipe.held }) : ""}
				>
					<StatsFunnel funnel={pipe.funnel} />
				</StatsCard>
				<StatsCard title={i18n.stats_failures()}>
					<StatsFailures failures={pipe.failures} />
				</StatsCard>
			</div>

			<StatsCard title={i18n.settings_indexers()} flush>
				<StatsIndexers indexers={pipe.indexers} />
			</StatsCard>

			<div class="grid grid-cols-1 gap-4 md:grid-cols-[2fr_1fr]">
				<StatsCard title={i18n.transcode_label()} caption={i18n.stats_busy({ time: fmtWait(pipe.transcode.busy_s) })}>
					<div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
						<StatsFigure size="sm" label={i18n.status_succeeded()} value={fmtCount(pipe.transcode.succeeded)} />
						<StatsFigure size="sm" label={i18n.status_failed()} value={fmtCount(pipe.transcode.failed)} tone={pipe.transcode.failed > 0 ? "failed" : ""} />
						<StatsFigure size="sm" label={i18n.status_rejected()} value={fmtCount(pipe.transcode.rejected)} />
						<StatsFigure size="sm" label={i18n.stats_saved()} value={fmtSize(pipe.transcode.saved_bytes)} tone="available" />
					</div>
				</StatsCard>
				<StatsCard title={i18n.stats_drift()}>
					<div class="grid grid-cols-2 gap-4">
						<StatsFigure size="sm" label={i18n.stats_drift_detected()} value={fmtCount(pipe.drift.detected)} />
						<StatsFigure size="sm" label={i18n.stats_drift_confirmed()} value={fmtCount(pipe.drift.confirmed)} tone={pipe.drift.confirmed > 0 ? "wanted" : ""} />
					</div>
				</StatsCard>
			</div>
		</section>

		<!-- Users & requests -->
		<section aria-labelledby="stats-users" class="flex flex-col gap-4">
			<header>
				<h2 id="stats-users" class={sectionHead}>{i18n.stats_section_users()}</h2>
				<p class={sectionLede}>
					{i18n.stats_users_summary({
						requests: fmtCount(users.requests.total),
						people: fmtCount(users.people.filter((p) => p.requests > 0).length),
						pending: fmtCount(users.requests.pending),
					})}
				</p>
			</header>
			<div class="grid grid-cols-2 gap-x-4 gap-y-5 md:grid-cols-4">
				<StatsFigure
					size="lg"
					label={i18n.requests_label()}
					value={fmtCount(users.requests.total)}
					delta={delta(t.requests, prev.requests)}
					sub={i18n.stats_active_of({ active: users.active, total: users.total })}
				/>
				<StatsFigure
					size="lg"
					label={i18n.common_pending()}
					value={fmtCount(users.requests.pending)}
					tone={users.requests.pending > 0 ? "wanted" : ""}
				/>
				<StatsFigure size="lg" label={i18n.stats_decide_time()} value={fmtWait(users.decide_p50_s)} sub={i18n.stats_median()} />
				<StatsFigure size="lg" label={i18n.stats_available_time()} value={fmtWait(users.available_p50_s)} sub={i18n.stats_median()} />
			</div>
			<StatsCard title={i18n.stats_people()} caption={range} flush>
				<StatsPeople people={users.people} />
			</StatsCard>
		</section>
	</div>
</div>
