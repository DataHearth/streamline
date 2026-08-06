<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Check, CalendarCog } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type { LibraryConfig } from "../../lib/types";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

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
				toast.ok("Series settings saved");
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	type ApplyResult = { seasons_updated: number; monitored: boolean };

	const applyExisting = createMutation<ApplyResult, Error, void>(() => ({
		mutationFn: () =>
			api<ApplyResult>("/series/specials/apply", { method: "POST" }),
		onSuccess: ({ seasons_updated: n, monitored }) => {
			qc.invalidateQueries({ queryKey: ["series"] });
			const verb = monitored ? "monitored" : "unmonitored";
			toast.ok(
				n === 0
					? i18n.series_specials_none()
					: n === 1 ? i18n.series_specials_applied_one({ verb, count: n }) : i18n.series_specials_applied_other({ verb, count: n }),
			);
		},
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_series()}</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{i18n.settings_series_intro()}
		</p>
	</header>

	{#if cfg.isPending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(cfg.error) })}
		</p>
	{:else if cfg.data}
		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<Checkbox
				checked={cfg.data.monitor_specials}
				disabled={config.readOnly || save.isPending}
				onChange={(v) => save.mutate({ monitor_specials: v })}
				label={i18n.series_monitor_specials()}
				description={i18n.series_specials_help()}
			/>
			<div
				class="mt-3 border-t border-border pt-3 text-xs text-fg-subtle"
			>
				<p class="flex items-center gap-1.5">
					<Check size={12} aria-hidden="true" />
					{i18n.settings_series_applies()}
				</p>
				<div class="mt-2.5 flex flex-wrap items-center gap-2">
					<button
						type="button"
						disabled={applyExisting.isPending}
						onclick={() => applyExisting.mutate()}
						class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-bg px-2.5 text-xs font-medium text-fg transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-60"
					>
						<CalendarCog size={13} aria-hidden="true" />
						{applyExisting.isPending
							? i18n.common_applying()
							: i18n.series_apply_existing()}
					</button>
					<span>
						{cfg.data.monitor_specials
							? i18n.series_specials_switch()
							: i18n.series_specials_switch_off()}
					</span>
				</div>
			</div>
		</section>
	{/if}
</div>
