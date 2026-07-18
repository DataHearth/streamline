<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import { api } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { scheduleInterval } from "../../lib/schemas";
	import type { Schedule, ScheduleList } from "../../lib/types";
	import Modal from "../../components/modals/Modal.svelte";
	import ScheduleRow from "../../components/settings/ScheduleRow.svelte";
	import TextField from "../../components/forms/TextField.svelte";

	const JOB_DESCRIPTIONS: Record<string, string> = {
		"rss-sync": "Pull configured RSS feeds for new releases",
		"missing-search":
			"Per-title indexer search for every wanted movie past cooldown",
		"tv-missing-search":
			"Per-season indexer search for wanted episodes past cooldown",
		"metadata-refresh": "Re-fetch TMDB metadata for tracked movies",
		"tv-metadata-refresh": "Re-fetch TVDB metadata for tracked series",
		"download-monitor":
			"Track active torrents, hand finished ones to the importer",
		"import-scan": "Walk the import directory and stage matched files",
		cleanup: "Purge old download records past their retention window",
		"purge-sessions": "Drop expired auth sessions from the DB",
		"orphan-scan":
			"Walk the library for untracked media and classify against TMDB",
		"drift-check":
			"Verify tracked files still exist on disk; revert missing movies to wanted",
	};

	const qc = useQueryClient();

	const list = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		refetchInterval: 10_000,
	}));

	let editing = $state<Schedule | null>(null);
	let modalOpen = $state(false);

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
		onError: (err) => toast.err(err.message),
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
</script>

<div>
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">Schedules</h1>
		<p class="mt-1 text-sm text-fg-muted">
			Background jobs Streamline runs on a fixed interval. Edit the cadence,
			pause, or trigger a run on demand.
		</p>
	</header>

	{#if list.isPending}
		<p class="mt-6 text-sm text-fg-subtle">Loading…</p>
	{:else if list.isError}
		<p class="mt-6 text-sm text-status-failed">
			Failed to load: {list.error?.message}
		</p>
	{:else if items.length === 0}
		<p class="mt-6 text-sm text-fg-muted">No schedules registered.</p>
	{:else}
		<div
			class="mt-6 overflow-x-auto rounded-lg border border-border bg-bg-elevated"
		>
			<table class="w-full text-sm">
				<thead
					class="bg-surface text-left text-xs uppercase tracking-wider text-fg-muted"
				>
					<tr>
						<th class="px-4 py-2.5 font-semibold">Job</th>
						<th class="px-4 py-2.5 font-semibold">Interval</th>
						<th class="px-4 py-2.5 font-semibold">Last run</th>
						<th class="px-4 py-2.5 font-semibold">Status</th>
						<th class="px-4 py-2.5 font-semibold">Next run</th>
						<th class="px-4 py-2.5 text-right font-semibold">Actions</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-border">
					{#each userItems as s (s.name)}
						<ScheduleRow
							row={s}
							description={JOB_DESCRIPTIONS[s.name]}
							onEdit={openEdit}
						/>
					{/each}
				</tbody>
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
					System
				</span>
				<div class="h-px flex-1 bg-border"></div>
				<span
					class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint"
				>
					Predefined · interval not editable
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
							<th class="px-4 py-2.5 font-semibold">Job</th>
							<th class="px-4 py-2.5 font-semibold">Interval</th>
							<th class="px-4 py-2.5 font-semibold">Last run</th>
							<th class="px-4 py-2.5 font-semibold">Status</th>
							<th class="px-4 py-2.5 font-semibold">Next run</th>
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
	{/if}

	<p class="mt-3 text-xs text-fg-subtle">
		Tip: intervals are Go duration strings (e.g.
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>15m</code
		>,
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>24h</code
		>,
		<code class="rounded bg-bg-card px-1.5 py-0.5 font-mono text-fg-muted"
			>30s</code
		>) — minimum 10 seconds.
	</p>
</div>

<Modal
	open={modalOpen}
	title={editing ? `Edit interval — ${editing.name}` : "Edit interval"}
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
		<form.Field name="interval">
			{#snippet children(field)}
				<TextField
					{field}
					label="Interval"
					placeholder="15m"
					help="Go duration string. Minimum 10 seconds."
				/>
			{/snippet}
		</form.Field>
	</form>

	{#snippet footer()}
		<button
			type="button"
			onclick={() => (modalOpen = false)}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm text-fg-muted hover:text-fg"
		>
			Cancel
		</button>
		<button
			type="submit"
			form="schedule-form"
			disabled={config.readOnly || !form.state.canSubmit || form.state.isSubmitting}
			class="inline-flex h-9 items-center rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{form.state.isSubmitting ? "Saving…" : "Save"}
		</button>
	{/snippet}
</Modal>
