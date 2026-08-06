<script lang="ts">
	import {
		ChevronRight,
		Folder,
		FolderOpen,
		FileText,
	} from "@lucide/svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import { cn } from "../../lib/cn";
	import { formatBytes } from "../../lib/format";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import type {
		TorrentFile,
		TorrentFilePriority,
		TorrentStatus,
	} from "../../lib/types";

	let {
		files,
		status,
		canControl = false,
		busyIndex = null,
		onSetPriority,
	}: {
		files: TorrentFile[];
		status: TorrentStatus;
		canControl?: boolean;
		busyIndex?: number | null;
		onSetPriority: (index: number, priority: TorrentFilePriority) => void;
	} = $props();

	const ROW_H = 46;
	const VIRT_THRESHOLD = 50;
	// Each priority owns a colour, and the unselected buttons carry it as a dot, so
	// the mapping is legible before you've clicked anything: slate = not wanted,
	// accent = normal, amber = ahead of the rest.
	const PRIOS: {
		key: TorrentFilePriority;
		label: string;
		on: string;
		dot: string;
	}[] = [
		{
			key: "skip",
			label: i18n.common_skip(),
			on: "bg-status-missing/25 text-fg-subtle",
			dot: "bg-status-missing",
		},
		{
			key: "normal",
			label: i18n.common_normal(),
			on: "bg-accent-soft text-accent-text",
			dot: "bg-accent",
		},
		{
			key: "high",
			label: i18n.common_high(),
			on: "bg-status-wanted/20 text-status-wanted",
			dot: "bg-status-wanted",
		},
	];
	const PRIO_TEXT: Record<TorrentFilePriority, string> = {
		skip: "bg-status-missing/25 text-fg-subtle",
		normal: "bg-accent-soft text-accent-text",
		high: "bg-status-wanted/20 text-status-wanted",
	};

	type FileEntry = { path: string; size: number; progress: number; priority: TorrentFilePriority; index: number; fname: string };
	type Node = { name: string; path: string; dirs: Map<string, Node>; files: FileEntry[] };

	function buildTree(src: TorrentFile[]): Node {
		const root: Node = { name: "", path: "", dirs: new Map(), files: [] };
		for (const f of src) {
			const parts = f.path.split("/");
			const fname = parts.pop() as string;
			let node = root;
			let acc = "";
			for (const p of parts) {
				acc = acc ? acc + "/" + p : p;
				if (!node.dirs.has(p))
					node.dirs.set(p, { name: p, path: acc, dirs: new Map(), files: [] });
				node = node.dirs.get(p) as Node;
			}
			node.files.push({
				path: f.path,
				size: f.size,
				progress: f.size > 0 ? f.downloaded / f.size : 0,
				priority: f.priority,
				index: f.index,
				fname,
			});
		}
		return root;
	}

	function agg(node: Node): { count: number; size: number; done: number } {
		let count = 0, size = 0, done = 0;
		for (const f of node.files) {
			count++;
			if (f.priority !== "skip") {
				size += f.size;
				done += f.size * f.progress;
			}
		}
		for (const d of node.dirs.values()) {
			const a = agg(d);
			count += a.count;
			size += a.size;
			done += a.done;
		}
		return { count, size, done };
	}

	type Row =
		| { type: "folder"; path: string; name: string; depth: number; count: number; size: number; done: number }
		| { type: "file"; depth: number; file: FileEntry };

	function flatten(root: Node, collapsed: Set<string>): Row[] {
		const rows: Row[] = [];
		function walk(node: Node, depth: number) {
			const dirs = [...node.dirs.values()].sort((a, b) => a.name.localeCompare(b.name));
			for (const d of dirs) {
				const a = agg(d);
				rows.push({ type: "folder", path: d.path, name: d.name, depth, count: a.count, size: a.size, done: a.done });
				if (!collapsed.has(d.path)) walk(d, depth + 1);
			}
			const list = node.files.slice().sort((a, b) => a.fname.localeCompare(b.fname));
			for (const f of list) rows.push({ type: "file", depth, file: f });
		}
		walk(root, 0);
		return rows;
	}

	function allFolders(node: Node, acc: string[] = []): string[] {
		for (const d of node.dirs.values()) {
			acc.push(d.path);
			allFolders(d, acc);
		}
		return acc;
	}

	let collapsed = $state(new Set<string>());
	let tree = $derived(buildTree(files));
	let rows = $derived(flatten(tree, collapsed));
	let virtual = $derived(rows.length >= VIRT_THRESHOLD);
	let total = $derived(agg(tree));

	let scrollTop = $state(0);
	const VIEW_H = 440;
	let start = $derived(virtual ? Math.max(0, Math.floor(scrollTop / ROW_H) - 4) : 0);
	let end = $derived(
		virtual ? Math.min(rows.length, Math.ceil((scrollTop + VIEW_H) / ROW_H) + 4) : rows.length,
	);
	let visible = $derived(rows.slice(start, end));

	function toggle(path: string) {
		const next = new Set(collapsed);
		if (next.has(path)) next.delete(path);
		else next.add(path);
		collapsed = next;
	}
	function collapseAll() {
		collapsed = new Set(allFolders(tree));
	}
	function expandAll() {
		collapsed = new Set();
	}
	let allCollapsed = $derived(allFolders(tree).length > 0 && allFolders(tree).every((p) => collapsed.has(p)));

	// ── Folder-level priority ────────────────────────────────────────────────
	// A season pack is one decision, not twenty-four. There is no bulk endpoint in
	// the contract, so this fans out one PATCH per file under the folder; the parent
	// invalidates the detail query once for the batch.
	function nodeAt(path: string): Node | null {
		let node: Node | null = tree;
		for (const part of path.split("/")) {
			node = node?.dirs.get(part) ?? null;
			if (!node) return null;
		}
		return node;
	}
	function filesUnder(node: Node, acc: FileEntry[] = []): FileEntry[] {
		acc.push(...node.files);
		for (const d of node.dirs.values()) filesUnder(d, acc);
		return acc;
	}
	// Null when the folder's files disagree — no option reads as selected then.
	function folderPriority(path: string): TorrentFilePriority | null {
		const node = nodeAt(path);
		if (!node) return null;
		const all = filesUnder(node);
		if (all.length === 0) return null;
		const first = all[0].priority;
		return all.every((f) => f.priority === first) ? first : null;
	}
	function setFolderPriority(path: string, priority: TorrentFilePriority) {
		const node = nodeAt(path);
		if (!node) return;
		for (const f of filesUnder(node)) {
			if (f.priority !== priority) onSetPriority(f.index, priority);
		}
	}
</script>

<div class="flex items-center justify-between gap-2 pb-2">
	<p class="text-[11px] text-fg-faint">
		<span class="font-mono tabular-nums text-fg-muted">{total.count}</span>
		files ·
		<span class="font-mono tabular-nums text-fg-muted">{formatBytes(total.size)}</span>
		wanted{virtual ? " · virtualized" : ""}
	</p>
	{#if allFolders(tree).length > 0}
		<button
			type="button"
			onclick={() => (allCollapsed ? expandAll() : collapseAll())}
			class="text-[11px] font-medium text-fg-subtle transition hover:text-fg"
		>
			{allCollapsed ? i18n.common_expand_all() : i18n.common_collapse_all()}
		</button>
	{/if}
</div>

<div
	class="relative overflow-y-auto rounded-md border border-border"
	style:height={virtual ? VIEW_H + "px" : "auto"}
	onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
>
	<div class="relative" style:height={virtual ? rows.length * ROW_H + "px" : "auto"}>
		{#each visible as row, i (row.type === "folder" ? "d:" + row.path : "f:" + row.file.index)}
			<div
				class={cn(
					"flex items-center gap-2 border-b border-border/50 px-3",
					virtual && "absolute inset-x-0",
				)}
				style:height="{ROW_H}px"
				style:top={virtual ? (start + i) * ROW_H + "px" : undefined}
				style:padding-left={12 + row.depth * 16 + "px"}
			>
				{#if row.type === "folder"}
					<button
						type="button"
						onclick={() => toggle(row.path)}
						class="flex min-w-0 flex-1 items-center gap-2 text-left"
						aria-expanded={!collapsed.has(row.path)}
					>
						<ChevronRight
							size={14}
							class={cn(
								"shrink-0 text-fg-faint transition-transform motion-safe:duration-150",
								!collapsed.has(row.path) && "rotate-90",
							)}
							aria-hidden="true"
						/>
						{#if collapsed.has(row.path)}
							<Folder size={15} class="shrink-0 text-fg-subtle" aria-hidden="true" />
						{:else}
							<FolderOpen size={15} class="shrink-0 text-accent-text" aria-hidden="true" />
						{/if}
						<!-- The name yields, the numbers don't: a truncated folder size or
						     percentage is unreadable, a truncated folder name still is. -->
						<span class="min-w-0 flex-1 truncate text-xs font-semibold text-fg">
							{row.name}
						</span>
						<span
							class="ml-1 shrink-0 whitespace-nowrap font-mono text-[10px] tabular-nums text-fg-faint"
						>
							{row.count} · {formatBytes(row.size)}
							{#if row.size > 0}· {Math.round((row.done / row.size) * 100)}%{/if}
						</span>
					</button>
					{#if canControl}
						{@const fp = folderPriority(row.path)}
						<div
							class="inline-flex shrink-0 overflow-hidden rounded-md border border-border"
							role="radiogroup"
							aria-label="Priority for everything in {row.name}"
						>
							{#each PRIOS as p (p.key)}
								{@const sel = fp === p.key}
								<button
									type="button"
									aria-pressed={sel}
									aria-label="{p.label} — all {row.count} files in {row.name}"
									title="{p.label} — all {row.count} files in {row.name}"
									onclick={() => setFolderPriority(row.path, p.key)}
									class={cn(
										"flex items-center gap-1.5 px-2 py-1 text-[10px] font-semibold uppercase tracking-wide transition",
										sel ? p.on : "text-fg-faint hover:bg-surface hover:text-fg",
									)}
								>
									<span
										class={cn(
											"h-1.5 w-1.5 shrink-0 rounded-full",
											p.dot,
											!sel && "opacity-50",
										)}
										aria-hidden="true"
									></span>
									<!-- Only the picked option spells itself out. Three labelled
									     buttons per row pushed the size and percentage under them in
									     the drawer; the dots carry the rest, and the title names them. -->
									{#if sel}{p.label}{/if}
								</button>
							{/each}
						</div>
					{/if}
				{:else}
					{@const f = row.file}
					{@const skip = f.priority === "skip"}
					<FileText
						size={14}
						class={cn("shrink-0", skip ? "text-fg-faint" : "text-fg-subtle")}
						aria-hidden="true"
					/>
					<span
						class={cn(
							"min-w-0 flex-1 truncate text-xs",
							skip ? "text-fg-faint line-through" : "text-fg-muted",
						)}
						title={f.fname}
					>
						{f.fname}
					</span>
					<div class="hidden w-24 items-center gap-1.5 sm:flex">
						{#if !skip}
							<div class="w-12">
								<ProgressBar value={f.progress} {status} height={2} />
							</div>
							<span class="font-mono tabular-nums text-[10px] text-fg-faint">
								{Math.round(f.progress * 100)}%
							</span>
						{/if}
					</div>
					<span class="w-16 shrink-0 text-right font-mono tabular-nums text-[11px] text-fg-subtle">
						{formatBytes(f.size)}
					</span>
					{#if canControl}
						<div
							class={cn(
								"inline-flex shrink-0 overflow-hidden rounded-md border border-border",
								busyIndex === f.index && "opacity-50",
							)}
							role="radiogroup"
							aria-label={i18n.common_priority()}
						>
							{#each PRIOS as p (p.key)}
								{@const sel = f.priority === p.key}
								<button
									type="button"
									aria-pressed={sel}
									aria-label={p.label}
									title={p.label}
									disabled={busyIndex === f.index}
									onclick={() => onSetPriority(f.index, p.key)}
									class={cn(
										"flex items-center gap-1.5 px-2 py-1 text-[10px] font-semibold uppercase tracking-wide transition",
										sel ? p.on : "text-fg-faint hover:bg-surface hover:text-fg",
									)}
								>
									<span
										class={cn(
											"h-1.5 w-1.5 shrink-0 rounded-full",
											p.dot,
											!sel && "opacity-50",
										)}
										aria-hidden="true"
									></span>
									{#if sel}{p.label}{/if}
								</button>
							{/each}
						</div>
					{:else}
						<span
							class={cn(
								"shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
								PRIO_TEXT[f.priority],
							)}
						>
							{f.priority}
						</span>
					{/if}
				{/if}
			</div>
		{/each}
	</div>
</div>
