<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { api, apiAllPages, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { formatDateShort } from "@lib/dates";
	import { toast } from "@lib/toast";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import type { MissingFile } from "@lib/types";
	import Dialog from "@components/modals/Dialog.svelte";

	let { rootLabels }: { rootLabels: Record<MissingFile["root"], () => string> } = $props();

	const FORGET_BATCH = 100;
	const QUERY_KEY = ["library", "missing-files"];

	const qc = useQueryClient();

	const missing = createQuery(() => ({
		queryKey: QUERY_KEY,
		queryFn: () => apiAllPages<MissingFile>("/library/missing-files"),
		enabled: auth.isAdmin,
	}));
	let files = $derived(missing.data?.items ?? []);
	let forgetting = $state<MissingFile[] | null>(null);

	const href = (f: MissingFile) => {
		if (f.owner_kind === "movie") return `/movies/${f.owner_id}`;
		if (f.owner_kind === "series") return `/series/${f.owner_id}`;
		if (f.owner_kind === "artist") return `/music/${f.owner_id}`;
		return `/books/${f.owner_id}`;
	};

	const forget = createMutation<number, Error, MissingFile[]>(() => ({
		mutationFn: async (rows) => {
			let forgotten = 0;
			for (let i = 0; i < rows.length; i += FORGET_BATCH) {
				const ids = rows.slice(i, i + FORGET_BATCH).map((f) => f.id);
				const resp = await api<{ forgotten: number }>("/library/missing-files/forget", {
					method: "POST",
					body: { ids },
				});
				forgotten += resp.forgotten;
			}
			return forgotten;
		},
		onSuccess: (forgotten) => toast.ok(i18n.library_missing_forgotten({ count: forgotten })),
		onError: (err) => toast.err(errorText(err)),
		onSettled: () => {
			forgetting = null;
			qc.invalidateQueries();
		},
	}));
</script>

{#if files.length > 0}
	<section class="mt-4 rounded-lg border border-border bg-bg-card p-4">
		<div class="flex flex-wrap items-start justify-between gap-2">
			<div class="min-w-0">
				<h2 class="text-sm font-semibold text-fg">{i18n.file_missing_from_disk()}</h2>
				<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">{i18n.library_missing_help()}</p>
			</div>
			<button
				type="button"
				class="rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-fg hover:border-border-strong"
				onclick={() => (forgetting = files)}
			>
				{i18n.library_missing_forget_all()}
			</button>
		</div>
		<ul class="mt-3 max-h-96 divide-y divide-border overflow-y-auto">
			{#each files as f (f.id)}
				<li class="flex items-start justify-between gap-3 py-2.5">
					<div class="min-w-0">
						<a href={href(f)} class="block break-words text-sm font-medium text-fg hover:text-accent-text">
							{f.title}
						</a>
						<p class="mt-0.5 break-all font-mono text-[11px] text-fg-subtle">{f.path}</p>
						<p class="mt-0.5 text-xs text-fg-muted">
							{rootLabels[f.root]()} · {f.redownload_at
								? i18n.library_missing_redownload({ date: formatDateShort(f.redownload_at) })
								: i18n.library_missing_never()}
						</p>
					</div>
					<button
						type="button"
						class="touch-hit shrink-0 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-fg hover:border-border-strong"
						onclick={() => (forgetting = [f])}
					>
						{i18n.library_missing_forget()}
					</button>
				</li>
			{/each}
		</ul>
	</section>
{/if}

<Dialog
	open={forgetting !== null}
	title={forgetting?.length === 1
		? i18n.library_missing_forget_title_one()
		: i18n.library_missing_forget_title_other({ count: forgetting?.length ?? 0 })}
	body={i18n.library_missing_forget_body()}
	onClose={() => (forgetting = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost" },
		{
			label: i18n.library_missing_forget(),
			variant: "danger",
			dismiss: false,
			pending: forget.isPending,
			onClick: () => forgetting && forget.mutate(forgetting),
		},
	]}
/>
