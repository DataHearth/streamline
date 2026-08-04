<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Check, CalendarCog } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type { LibraryConfig } from "../../lib/types";
	import Checkbox from "../../components/forms/Checkbox.svelte";

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
			onError: (err) => toast.err(err.message),
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
					? "No specials seasons to update"
					: `Specials ${verb} on ${n} season${n === 1 ? "" : "s"}`,
			);
		},
		onError: (err) => toast.err(err.message),
	}));
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">Series</h1>
		<p class="mt-1 text-sm text-fg-muted">
			How new series and seasons enter the library. Changes take effect
			immediately — no restart required.
		</p>
	</header>

	{#if cfg.isPending}
		<p class="mt-6 text-sm text-fg-subtle">Loading…</p>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			Failed to load series settings: {cfg.error?.message}
		</p>
	{:else if cfg.data}
		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<Checkbox
				checked={cfg.data.monitor_specials}
				disabled={config.readOnly || save.isPending}
				onChange={(v) => save.mutate({ monitor_specials: v })}
				label="Monitor specials"
				description="Season 0 holds recaps, OVAs and behind-the-scenes extras. Left off,
				a new series arrives with its specials unmonitored — nothing is searched
				or grabbed for them until you turn the season on."
			/>
			<div
				class="mt-3 border-t border-border pt-3 text-xs text-fg-subtle"
			>
				<p class="flex items-center gap-1.5">
					<Check size={12} aria-hidden="true" />
					Applies to series added — or seasons discovered — from now on.
					Seasons already in your library keep their current setting.
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
							? "Applying…"
							: "Apply to existing series"}
					</button>
					<span>
						{cfg.data.monitor_specials
							? "Switches season 0 on for every monitored series already in the library."
							: "Switches season 0 off for every series already in the library."}
					</span>
				</div>
			</div>
		</section>
	{/if}
</div>
