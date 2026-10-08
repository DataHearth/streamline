<script lang="ts">
	import { onMount } from "svelte";
	import { TriangleAlert } from "@lucide/svelte";
	import { errorText } from "@lib/api";
	import { appLabel, checkPaths } from "@lib/arr-import";
	import { cn } from "@lib/cn";
	import { INPUT_CLASS } from "@lib/form";
	import type {
		ArrApp,
		ArrRootCheck,
		ArrRootFolder,
		ArrRootMapping,
	} from "@lib/types";
	import ArrPill from "./ArrPill.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		app: ArrApp;
		folders: ArrRootFolder[];
		roots: ArrRootMapping[];
		checks: ArrRootCheck[];
		// True from the first keystroke until the debounced check answers, so the
		// wizard holds Next through the debounce window too.
		checking: boolean;
	};

	let {
		app,
		folders,
		roots = $bindable(),
		checks = $bindable(),
		checking = $bindable(),
	}: Props = $props();

	let label = $derived(appLabel(app));
	let error = $state<string | null>(null);

	// Every check covers all rows, so only the newest request may write back:
	// an older one answering late describes mappings that have since changed.
	let seq = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function run() {
		const mine = ++seq;
		checking = true;
		try {
			const res = await checkPaths($state.snapshot(roots));
			if (mine !== seq) return;
			checks = res;
			error = null;
		} catch (err) {
			if (mine !== seq) return;
			checks = [];
			error = errorText(err);
		} finally {
			if (mine === seq) checking = false;
		}
	}

	function edit(i: number, to: string) {
		const r = roots[i];
		if (!r) return;
		roots[i] = { ...r, to };
		checking = true;
		clearTimeout(timer);
		timer = setTimeout(run, 300);
	}

	onMount(() => {
		run();
		return () => {
			clearTimeout(timer);
			seq++;
		};
	});

	function folderFor(r: ArrRootMapping): ArrRootFolder | undefined {
		return folders.find((f) => f.path === r.from);
	}

	type Verdict = { tone: "ok" | "bad" | "neutral"; label: string; title?: string };

	function verdict(r: ArrRootMapping): Verdict | null {
		const c = checks.find((c) => c.from === r.from && c.to === r.to);
		if (!c) {
			return checking ? { tone: "neutral", label: i18n.arr_path_checking() } : null;
		}
		if (c.found) {
			return { tone: "ok", label: i18n.arr_path_found(), title: c.resolved };
		}
		const why =
			c.reason === "permission denied"
				? i18n.arr_path_permission_denied()
				: c.reason === "unreadable"
					? i18n.arr_path_unreadable()
					: i18n.arr_path_not_found();
		return { tone: "bad", label: why, title: c.resolved };
	}
</script>

<div class="space-y-4">
	<p class="text-sm text-fg-muted">{i18n.arr_paths_intro({ app: label })}</p>

	{#if error}
		<p role="alert" class="text-sm break-words text-status-failed">{error}</p>
	{/if}

	<ul class="space-y-3">
		{#each roots as r, i (r.from)}
			{@const folder = folderFor(r)}
			{@const v = r.sample_path ? verdict(r) : null}
			{@const count = folder?.title_count ?? 0}
			<li class="space-y-3 rounded-md border border-border bg-bg-elevated p-3">
				<div class="flex min-w-0 items-start justify-between gap-3">
					<div class="min-w-0">
						<p class="text-xs text-fg-subtle">
							{i18n.arr_path_source({ app: label })}
						</p>
						<p class="truncate font-mono text-[13px] text-fg" title={r.from}>
							{r.from}
						</p>
						<p class="mt-0.5 text-xs text-fg-muted">
							{count === 1
								? i18n.arr_path_titles_one({ count })
								: i18n.arr_path_titles_other({ count })}
						</p>
					</div>
					{#if v}
						<ArrPill tone={v.tone} label={v.label} title={v.title} />
					{/if}
				</div>

				{#if folder && !folder.accessible}
					<p class="flex items-start gap-1.5 text-xs text-status-held">
						<TriangleAlert size={13} class="mt-px shrink-0" aria-hidden="true" />
						<span>{i18n.arr_path_inaccessible({ app: label })}</span>
					</p>
				{/if}

				<label class="block">
					<span class="mb-1 block text-sm font-medium text-fg"
						>{i18n.arr_path_local()}</span
					>
					<input
						type="text"
						autocomplete="off"
						spellcheck="false"
						value={r.to}
						oninput={(e) => edit(i, (e.currentTarget as HTMLInputElement).value)}
						class={cn(INPUT_CLASS, "font-mono")}
					/>
				</label>

				{#if !r.sample_path}
					<p class="text-xs text-fg-subtle">{i18n.arr_path_no_sample()}</p>
				{/if}
			</li>
		{/each}
	</ul>
</div>
