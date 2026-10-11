<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Check, ListChecks } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config, markConfigForm } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import type { LibraryConfig } from "@lib/types";
	import { releaseTypeLabel, type ReleaseType } from "@lib/music-books";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Saves per control, as on /settings/series, so nothing else establishes the
	// config-form context the field primitives read.
	markConfigForm();

	const TYPES: ReleaseType[] = ["album", "ep", "single", "live", "compilation", "other"];

	const qc = useQueryClient();

	const cfg = createQuery<LibraryConfig>(() => ({
		queryKey: ["config", "library"],
		queryFn: () => api<LibraryConfig>("/config/library"),
	}));

	const save = createMutation<LibraryConfig, Error, Partial<LibraryConfig>>(
		() => ({
			mutationFn: (body) =>
				api<LibraryConfig>("/config/library", { method: "PATCH", body }),
			onSuccess: (resp) => {
				qc.setQueryData(["config", "library"], resp);
				toast.ok(i18n.music_settings_saved());
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	// Kept in TYPES order, so the stored list reads the same however it was
	// ticked.
	function toggle(current: ReleaseType[], t: ReleaseType, on: boolean) {
		const next = TYPES.filter((x) => (x === t ? on : current.includes(x)));
		save.mutate({ music_monitored_types: next });
	}

	type ApplyResult = { monitored: number; unmonitored: number; types: ReleaseType[] };

	const applyExisting = createMutation<ApplyResult, Error, void>(() => ({
		mutationFn: () =>
			api<ApplyResult>("/music/monitored-types/apply", { method: "POST" }),
		onSuccess: ({ monitored: on, unmonitored: off }) => {
			qc.invalidateQueries({ queryKey: ["music"] });
			toast.ok(
				on + off === 0
					? i18n.music_types_none()
					: i18n.music_types_applied({ on, off }),
			);
		},
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<p class="text-sm text-fg-muted">
			{i18n.settings_music_intro()}
		</p>
	</header>

	{#if cfg.isPending}
		<div class="mt-6 space-y-4"><SkeletonList variant="panel" count={1} /></div>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(cfg.error) })}
		</p>
	{:else if cfg.data}
		{@const types = cfg.data.music_monitored_types ?? []}
		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-medium text-fg">{i18n.music_monitored_types()}</h2>
			<p class="mt-1 text-xs text-fg-subtle">{i18n.music_monitored_types_help()}</p>
			<div class="mt-3 grid gap-2 sm:grid-cols-2">
				{#each TYPES as t (t)}
					<Checkbox
						checked={types.includes(t)}
						disabled={config.readOnly || save.isPending}
						onChange={(v) => toggle(types, t, v)}
						label={releaseTypeLabel(t)}
					/>
				{/each}
			</div>
			<div
				class="mt-3 border-t border-border pt-3 text-xs text-fg-subtle"
			>
				<p class="flex items-center gap-1.5">
					<Check size={12} aria-hidden="true" />
					{i18n.settings_music_applies()}
				</p>
				<div class="mt-2.5 flex flex-wrap items-center gap-2">
					<button
						type="button"
						disabled={applyExisting.isPending}
						onclick={() => applyExisting.mutate()}
						class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-bg px-2.5 text-xs font-medium text-fg transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-60"
					>
						<ListChecks size={13} aria-hidden="true" />
						{applyExisting.isPending
							? i18n.common_applying()
							: i18n.music_apply_existing()}
					</button>
					<span>{i18n.music_apply_existing_help()}</span>
				</div>
			</div>
		</section>
	{/if}
</div>
