<script lang="ts">
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import { entryHeading, holdFileCount } from "../../lib/activity-touch";
	import type { HoldCheck, HoldReason, QueueEntry } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The decision a held download is waiting for. Three outcomes, two buttons:
	// the two destructive ones are one button whose label follows a checkbox,
	// which is how DeleteTitleDialog settled the same problem — two deletes a row
	// apart put the irreversible one a slip of the thumb from the safe one.
	let {
		item,
		pending = false,
		onResolve,
		onClose,
	}: {
		item: QueueEntry | null;
		pending?: boolean;
		onResolve: (action: "import" | "regrab" | "delete") => void;
		onClose: () => void;
	} = $props();

	// On by default, and reset on every open: after a bad grab a replacement is
	// almost always what you want, and it is the less final of the two deletes.
	// Never remembered — a box carrying the last download's answer is the one
	// mistake this dialog exists to prevent.
	let regrab = $state(true);
	let openedFor = $state<number | null>(null);
	$effect(() => {
		if (item && item.id !== openedFor) {
			openedFor = item.id;
			regrab = true;
		}
		if (!item) openedFor = null;
	});

	const CHECK_LABELS: Record<HoldCheck, () => string> = {
		resolution: () => i18n.hold_check_resolution(),
		duration: () => i18n.hold_check_duration(),
		codec: () => i18n.hold_check_codec(),
		corrupt: () => i18n.hold_check_corrupt(),
		always_ask: () => i18n.hold_check_always_ask(),
	};

	// `always_ask` holds a file nothing is wrong with, so it has no claim to
	// contrast against — rendering the pair would read as a fifth failure.
	const hasExpectation = (r: HoldReason) => r.check !== "always_ask";

	// The hold is on the download; the reasons are itemised per file. A season
	// pack therefore lists several files, each with its own findings.
	type FileGroup = { file: string; name: string; findings: HoldReason[] };
	let groups = $derived.by<FileGroup[]>(() => {
		const out = new Map<string, FileGroup>();
		for (const r of item?.hold_reasons ?? []) {
			let g = out.get(r.file);
			if (!g) {
				g = {
					file: r.file,
					// The prefix is the same for every file in a pack, so the name is
					// what tells them apart. Full path stays in the title.
					name: r.file.split("/").pop() || r.file,
					findings: [],
				};
				out.set(r.file, g);
			}
			g.findings.push(r);
		}
		return [...out.values()];
	});

	let fileCount = $derived(holdFileCount(item?.hold_reasons));
</script>

<Dialog
	open={item !== null}
	title={item ? entryHeading(item) : ""}
	size="lg"
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.hold_import_anyway(),
			variant: "ghost",
			dismiss: false,
			disabled: pending,
			onClick: () => onResolve("import"),
		},
		{
			label: regrab ? i18n.hold_delete_and_search() : i18n.common_delete(),
			variant: "danger",
			dismiss: false,
			pending,
			onClick: () => onResolve(regrab ? "regrab" : "delete"),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{fileCount === 1
			? i18n.hold_body_one()
			: i18n.hold_body_other({ count: fileCount })}
	</p>

	<div class="mt-4 flex flex-col gap-3">
		{#each groups as g (g.file)}
			<div class="overflow-hidden rounded-md border border-border">
				<div
					class="truncate border-b border-border bg-bg px-2.5 py-2 font-mono text-[11px] text-fg"
					title={g.file}
				>
					{g.name}
				</div>
				<dl class="divide-y divide-border">
					{#each g.findings as f (f.check)}
						<div
							class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 px-2.5 py-2"
						>
							<dt
								class="font-mono text-[10px] uppercase tracking-[0.1em] text-fg-faint"
							>
								{CHECK_LABELS[f.check]?.() ?? f.check}
							</dt>
							<dd
								class="flex items-baseline justify-end gap-2 text-right font-mono text-xs"
							>
								{#if hasExpectation(f)}
									{#if f.expected}
										<span class="text-fg-subtle line-through">{f.expected}</span>
										<span class="text-fg-faint" aria-hidden="true">→</span>
									{/if}
									<span style:color="var(--status-held)">{f.actual || "—"}</span>
								{/if}
							</dd>
						</div>
					{/each}
				</dl>
			</div>
		{/each}
	</div>

	<div class="mt-4 border-t border-border pt-3.5">
		<Checkbox
			checked={regrab}
			onChange={(v) => (regrab = v)}
			label={i18n.hold_search_replacement()}
			description={i18n.hold_search_replacement_help()}
		/>
	</div>
</Dialog>
