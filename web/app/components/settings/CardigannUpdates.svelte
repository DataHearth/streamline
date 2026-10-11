<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { ShieldAlert } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { CardigannUpdates } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Upstream changes to a configured built-in indexer's definition that
	// would move it to other domains or change its login: held until an
	// administrator reads the diff and decides.
	const qc = useQueryClient();
	const updates = createQuery<CardigannUpdates>(() => ({
		queryKey: ["cardigann", "updates"],
		queryFn: () => api<CardigannUpdates>("/cardigann/updates"),
	}));

	const decide = createMutation<
		null,
		Error,
		{ file: string; verb: "approve" | "reject" }
	>(() => ({
		mutationFn: ({ file, verb }) =>
			api<null>(`/cardigann/updates/${encodeURIComponent(file)}/${verb}`, {
				method: "POST",
			}),
		onSuccess: (_, { verb }) => {
			qc.invalidateQueries({ queryKey: ["cardigann"] });
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok(
				verb === "approve"
					? i18n.cardigann_update_approved()
					: i18n.cardigann_update_rejected(),
			);
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	let pending = $derived(updates.data?.pending ?? []);
</script>

{#if pending.length > 0}
	<section
		class="mt-6 rounded-lg border border-status-wanted/40 bg-status-wanted/5 p-5"
		aria-labelledby="cardigann-updates"
	>
		<h2
			id="cardigann-updates"
			class="flex items-center gap-2 text-sm font-semibold text-fg"
		>
			<ShieldAlert size={16} class="text-status-wanted" aria-hidden="true" />
			{i18n.cardigann_updates_title()}
		</h2>
		<p class="mt-1 text-xs text-fg-muted">{i18n.cardigann_updates_help()}</p>
		<ul class="mt-4 space-y-4">
			{#each pending as u (u.file)}
				<li class="rounded-md border border-border bg-bg-elevated p-4">
					<div class="flex flex-wrap items-start justify-between gap-3">
						<div class="min-w-0">
							<p class="text-sm font-semibold text-fg">{u.name}</p>
							<p class="mt-0.5 text-xs text-fg-muted">
								{i18n.cardigann_update_used_by({
									indexers: u.indexers.join(", "),
								})}
							</p>
							<ul class="mt-2 list-inside list-disc text-xs text-fg">
								{#each u.reasons as r (r)}
									<li>{r}</li>
								{/each}
							</ul>
						</div>
						<div class="flex shrink-0 gap-2">
							<button
								type="button"
								disabled={decide.isPending}
								onclick={() => decide.mutate({ file: u.file, verb: "reject" })}
								class="rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-xs font-semibold text-fg transition hover:border-border-strong disabled:opacity-60"
							>
								{i18n.cardigann_update_reject()}
							</button>
							<button
								type="button"
								disabled={decide.isPending}
								onclick={() => decide.mutate({ file: u.file, verb: "approve" })}
								class="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
							>
								{i18n.cardigann_update_approve()}
							</button>
						</div>
					</div>
					<details class="mt-3">
						<summary class="cursor-pointer text-xs text-fg-muted">
							{i18n.cardigann_update_diff()}
						</summary>
						<pre
							class="mt-2 max-h-96 overflow-auto rounded bg-bg-deep p-3 font-mono text-[11px] leading-snug">{#each u.diff.split("\n") as line, n (n)}<span
									class:text-status-available={line.startsWith("+")}
									class:text-status-failed={line.startsWith("-")}
									class:text-fg-muted={line.startsWith("@@")}
									>{line}
</span>{/each}</pre>
					</details>
				</li>
			{/each}
		</ul>
	</section>
{/if}
