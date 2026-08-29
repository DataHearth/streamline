<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { scheduleInterval } from "../../lib/schemas";
	import type { Schedule, ScheduleList } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import ScheduleRow from "../../components/settings/ScheduleRow.svelte";
	import ScheduleTouchList from "../../components/settings/ScheduleTouchList.svelte";
	import ScheduleActionSheet from "../../components/settings/ScheduleActionSheet.svelte";
	import TextField from "../../components/forms/TextField.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const JOB_DESCRIPTIONS: Record<string, string> = {
		"movie-rss-sync": i18n.schedule_rss_movies(),
		"tv-rss-sync": i18n.schedule_rss_series(),
		"movie-missing-search":
			i18n.schedule_search_movies(),
		"tv-missing-search":
			i18n.schedule_search_series(),
		"movie-metadata-refresh": i18n.schedule_refresh_movies(),
		"tv-metadata-refresh": i18n.schedule_refresh_series(),
		"movie-orphan-scan":
			i18n.schedule_scan_movies(),
		"tv-orphan-scan":
			i18n.schedule_scan_series(),
		"download-monitor":
			i18n.schedule_track_torrents(),
		"import-scan": i18n.schedule_import_walk(),
		"media-probe": i18n.schedule_media_probe(),
		cleanup: i18n.schedule_purge_downloads(),
		"purge-sessions": i18n.schedule_purge_sessions(),
		"drift-check":
			i18n.schedule_verify_files(),
	};

	const qc = useQueryClient();

	const list = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		refetchInterval: 10_000,
	}));

	let editing = $state<Schedule | null>(null);
	let modalOpen = $state(false);
	// Touch: the row's trailing ⋯ opens this instead of carrying three 28px
	// buttons. See ScheduleActionSheet.
	let menuJob = $state<Schedule | null>(null);

	const save = createMutation<Schedule, Error, { interval: string }>(() => ({
		mutationFn: (body) => {
			if (!editing) throw new Error("no row selected");
			return api<Schedule>(
				`/schedules/${encodeURIComponent(editing.name)}`,
				{ method: "PATCH", body },
			);
		},
		onSuccess: (resp) => {
			qc.setQueryData(
				["schedules"],
				(prev: ScheduleList | undefined) => ({
					items: (prev?.items ?? []).map((s) =>
						s.name === resp.name ? resp : s,
					),
				}),
			);
			toast.ok("Schedule updated");
			modalOpen = false;
			editing = null;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: { interval: "" },
		validators: { onChange: v.object({ interval: scheduleInterval }) },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function openEdit(s: Schedule) {
		editing = s;
		form.reset({ interval: s.interval });
		modalOpen = true;
	}

	let items = $derived(list.data?.items ?? []);
	let userItems = $derived(items.filter((s) => !s.system));
	let systemItems = $derived(items.filter((s) => s.system));

	// Fourteen jobs sorted by name interleave the two libraries — movie-rss-sync
	// sits between movie-orphan-scan and tv-metadata-refresh, so reading "what
	// runs for my series" means scanning the whole table. The name prefix is the
	// grouping the jobs already have; this just shows it.
	const JOB_GROUPS: { key: string; label: string; match: (n: string) => boolean }[] =
		[
			{
				key: "movies",
				label: i18n.movies_label(),
				match: (n) => n.startsWith("movie-"),
			},
			{
				key: "series",
				label: i18n.settings_series(),
				match: (n) => n.startsWith("tv-"),
			},
			{
				key: "downloads",
				label: i18n.schedules_group_downloads(),
				match: (n) => n === "download-monitor" || n === "import-scan",
			},
			{
				key: "maintenance",
				label: i18n.schedules_group_maintenance(),
				match: () => true,
			},
		];

	function groupSchedules(rows: Schedule[]) {
		return JOB_GROUPS.map((g, i) => ({
			...g,
			rows: rows.filter(
				(s) => JOB_GROUPS.findIndex((c) => c.match(s.name)) === i,
			),
		})).filter((g) => g.rows.length > 0);
	}

	let userGroups = $derived(groupSchedules(userItems));
</script>

<div>
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_schedules()}</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{i18n.schedules_intro()}
		</p>
	</header>

	{#if list.isPending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if list.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
		</p>
	{:else if items.length === 0}
		<p class="mt-6 text-sm text-fg-muted">{i18n.schedule_none()}</p>
	{:else}
		<ScheduleTouchList
			{items}
			descriptions={JOB_DESCRIPTIONS}
			onMenu={(s) => (menuJob = s)}
		/>

		<!-- The six-column table needs ~680px; below lg the settings content column
		     never has it (at 834 the old md sidebar left 430px), so it does not try. -->
		<div class="hidden lg:block">
		<div
			class="mt-6 overflow-x-auto rounded-lg border border-border bg-bg-elevated"
		>
			<table class="w-full text-sm">
				<thead
					class="bg-surface text-left text-xs uppercase tracking-wider text-fg-muted"
				>
					<tr>
						<th class="px-4 py-2.5 font-semibold">{i18n.schedule_job()}</th>
						<th class="px-4 py-2.5 font-semibold">{i18n.field_interval()}</th>
						<th class="px-4 py-2.5 font-semibold">{i18n.schedule_last_run()}</th>
						<th class="px-4 py-2.5 font-semibold">{i18n.common_status()}</th>
						<th class="px-4 py-2.5 font-semibold">{i18n.schedule_next_run()}</th>
						<th class="px-4 py-2.5 text-right font-semibold">{i18n.common_actions()}</th>
					</tr>
				</thead>
				{#each userGroups as g (g.key)}
					<tbody class="divide-y divide-border border-t border-border">
						<tr class="bg-surface/50">
							<th
								colspan="6"
								scope="colgroup"
								class="px-4 py-1.5 text-left font-mono text-[10px] uppercase tracking-[0.16em] text-fg-faint"
							>
								{g.label}
							</th>
						</tr>
						{#each g.rows as s (s.name)}
							<ScheduleRow
								row={s}
								description={JOB_DESCRIPTIONS[s.name]}
								onEdit={openEdit}
							/>
						{/each}
					</tbody>
				{/each}
			</table>
		</div>

		{#if systemItems.length > 0}
			<div
				class="my-4 flex items-center gap-3"
				aria-hidden="true"
			>
				<span
					class="font-mono text-[10px] uppercase tracking-[0.18em] text-fg-faint"
				>
					{i18n.settings_system()}
				</span>
				<div class="h-px flex-1 bg-border"></div>
				<span
					class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint"
				>
					{i18n.schedules_predefined()}
				</span>
			</div>

			<div
				class="overflow-x-auto rounded-lg border border-border bg-bg-elevated/60"
			>
				<table class="w-full text-sm">
					<thead
						class="bg-surface text-left text-xs uppercase tracking-wider text-fg-muted"
					>
						<tr>
							<th class="px-4 py-2.5 font-semibold">{i18n.schedule_job()}</th>
							<th class="px-4 py-2.5 font-semibold">{i18n.field_interval()}</th>
							<th class="px-4 py-2.5 font-semibold">{i18n.schedule_last_run()}</th>
							<th class="px-4 py-2.5 font-semibold">{i18n.common_status()}</th>
							<th class="px-4 py-2.5 font-semibold">{i18n.schedule_next_run()}</th>
							<th class="px-4 py-2.5 text-right font-semibold"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-border">
						{#each systemItems as s (s.name)}
							<ScheduleRow
								row={s}
								description={JOB_DESCRIPTIONS[s.name]}
								onEdit={openEdit}
							/>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		</div>
	{/if}

	<p class="mt-3 text-xs text-fg-subtle">
		{i18n.schedule_tip_prefix()}
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>15m</code
		>,
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>24h</code
		>,
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>30s</code
		>{i18n.schedule_tip_suffix()}
	</p>
</div>

<ScheduleActionSheet
	job={menuJob}
	onClose={() => (menuJob = null)}
	onEditInterval={openEdit}
/>

<Modal
	open={modalOpen}
	title={editing ? i18n.schedule_edit_interval_for({ name: editing.name }) : i18n.schedule_edit_interval()}
	size="md"
	onClose={() => (modalOpen = false)}
>
	<form
		id="schedule-form"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<ReadOnlyFieldset>
			<form.Field name="interval">
				{#snippet children(field)}
					<TextField
						{field}
						label={i18n.field_interval()}
						placeholder="15m"
						help={i18n.schedule_interval_help()}
					/>
				{/snippet}
			</form.Field>
		</ReadOnlyFieldset>
	</form>

	{#snippet footer()}
		<button
			type="button"
			onclick={() => (modalOpen = false)}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
		>
			{i18n.common_cancel()}
		</button>
		<button
			type="submit"
			form="schedule-form"
			disabled={config.readOnly || !form.state.canSubmit || form.state.isSubmitting}
			class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{form.state.isSubmitting ? i18n.common_saving() : i18n.common_save()}
		</button>
	{/snippet}
</Modal>
