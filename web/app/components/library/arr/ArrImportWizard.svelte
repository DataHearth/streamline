<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { goto } from "@roxi/routify";
	import { onMount, tick } from "svelte";
	import { SvelteMap } from "svelte/reactivity";
	import { ChevronLeft, ChevronRight, Play } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import {
		blockingRootCheck,
		defaultRoots,
		profileMappings,
		type ProfileChoice,
	} from "@lib/arr-import";
	import { cn } from "@lib/cn";
	import { toast } from "@lib/toast";
	import type {
		ApplySourceConfigResult,
		ArrApp,
		ArrPreview,
		ArrRootCheck,
		ArrRootMapping,
		ImportMode,
		ImportScan,
		ImportStartRequest,
		ImportTransferMode,
	} from "@lib/types";
	import ArrConnectStep from "./ArrConnectStep.svelte";
	import ArrPathsStep from "./ArrPathsStep.svelte";
	import ArrProfilesStep from "./ArrProfilesStep.svelte";
	import ArrConfigStep, { type ConfigPick } from "./ArrConfigStep.svelte";
	import ArrModeStep from "./ArrModeStep.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = { app: ArrApp; onCreated?: () => void };
	let { app, onCreated }: Props = $props();

	const qc = useQueryClient();

	// Same snapshot as NewImportForm: get(goto) inside a mutation callback
	// throws, and goto resolves route patterns, not concrete paths.
	let navigate: (path: string, params?: Record<string, string>) => void =
		() => {};
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	const TOTAL = 5;
	const STEPS = [
		i18n.arr_step_connect(),
		i18n.arr_step_paths(),
		i18n.arr_step_profiles(),
		i18n.arr_step_config(),
		i18n.arr_step_mode(),
	];

	// Everything lives in this component and nowhere else — the API key
	// included, which goes with the component when the sheet or modal closes.
	let step = $state(1);
	let url = $state("");
	let apiKey = $state("");
	let preview = $state<ArrPreview | null>(null);
	let roots = $state<ArrRootMapping[]>([]);
	let checks = $state<ArrRootCheck[]>([]);
	let checking = $state(false);
	const choices = new SvelteMap<number, ProfileChoice>();
	let indexerPicks = $state<Record<string, ConfigPick>>({});
	let clientPicks = $state<Record<string, ConfigPick>>({});
	let applied = $state<ApplySourceConfigResult | null>(null);
	let mode = $state<ImportMode>("in_place");
	let importMode = $state<"" | ImportTransferMode>("");

	// Drops everything a preview produced. A preview describes one instance
	// read with one key; once either changes, none of it is known to hold.
	function clearDerived() {
		preview = null;
		roots = [];
		checks = [];
		checking = false;
		choices.clear();
		indexerPicks = {};
		clientPicks = {};
		applied = null;
	}

	function onCredentials(u: string, k: string) {
		if (u === url && k === apiKey) return;
		url = u;
		apiKey = k;
		if (preview) {
			clearDerived();
			go(1);
		}
	}

	function onPreview(p: ArrPreview) {
		clearDerived();
		preview = p;
		roots = defaultRoots(p);
	}

	let heading = $state<HTMLElement | null>(null);

	async function go(n: number) {
		if (n === step) return;
		// A check in flight belongs to the paths step; leaving it abandons the
		// answer, and coming back re-runs the check on mount.
		if (step === 2) checking = false;
		step = n;
		await tick();
		heading?.focus();
	}

	let added = $derived(
		(applied?.indexers.length ?? 0) + (applied?.download_clients.length ?? 0),
	);

	let canNext = $derived.by(() => {
		switch (step) {
			case 1:
				return preview !== null;
			case 2:
				return !checking && !blockingRootCheck(checks, roots);
			default:
				return true;
		}
	});

	const start = createMutation<ImportScan, Error, ImportStartRequest>(() => ({
		mutationFn: (body) =>
			api<ImportScan>("/library/imports", { method: "POST", body }),
		onSuccess: (scan) => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok(i18n.arr_started());
			onCreated?.();
			navigate("/imports/[id]", { id: String(scan.id) });
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	function submit() {
		if (!preview) return;
		const body: ImportStartRequest = {
			source: app,
			source_url: url,
			api_key: apiKey,
			mode,
			// A root holding no titles maps nothing, and the server refuses any
			// mapping whose target does not exist on this host — so an empty
			// root the operator left at its identity default would block the
			// whole migration over a folder it never reads.
			root_mappings: $state.snapshot(roots).filter(
				(r) =>
					(preview?.root_folders.find((f) => f.path === r.from)?.title_count ??
						0) > 0,
			),
			profile_mappings: profileMappings(preview, choices),
		};
		if (mode === "rename" && importMode) body.import_mode = importMode;
		start.mutate(body);
	}

	const BTN =
		"inline-flex min-h-11 items-center justify-center gap-1.5 rounded-md px-4 text-sm font-semibold transition disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0";
	const PRIMARY = "bg-accent text-fg-on-accent hover:bg-accent-hover";
</script>

<div class="space-y-5">
	<!-- Five labels do not fit a 390px sheet, so the phone gets dots and the
	     current step's name, as ImportSteps does. -->
	<div>
		<p class="font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint">
			{i18n.arr_step_of({ current: step, total: TOTAL })}
		</p>
		<div class="mt-1.5 flex items-center gap-3 md:hidden">
			<div class="flex shrink-0 items-center gap-1.5" aria-hidden="true">
				{#each STEPS as s, i (s)}
					<span
						class={cn(
							"rounded-full transition-colors",
							i + 1 < step && "h-2 w-2 bg-status-available",
							i + 1 === step && "h-2.5 w-2.5 bg-accent ring-2 ring-accent-ring",
							i + 1 > step && "h-2 w-2 bg-border-strong",
						)}
					></span>
				{/each}
			</div>
		</div>
		<ol class="mt-1.5 hidden flex-wrap items-center gap-x-2 gap-y-1 md:flex">
			{#each STEPS as s, i (s)}
				<li
					class={cn(
						"flex items-center gap-2 text-[12.5px] font-semibold",
						i + 1 < step && "text-fg-muted",
						i + 1 === step && "text-accent-text",
						i + 1 > step && "text-fg-faint",
					)}
					aria-current={i + 1 === step ? "step" : undefined}
				>
					{s}
					{#if i < STEPS.length - 1}
						<ChevronRight size={12} class="text-fg-faint" aria-hidden="true" />
					{/if}
				</li>
			{/each}
		</ol>
		<h3
			bind:this={heading}
			tabindex="-1"
			class="mt-2 text-base font-semibold tracking-tight text-fg focus:outline-none"
		>
			{STEPS[step - 1]}
		</h3>
	</div>

	{#if step === 1}
		<ArrConnectStep {app} {url} {apiKey} {preview} {onCredentials} {onPreview} />
	{:else if step === 2 && preview}
		<ArrPathsStep
			{app}
			folders={preview.root_folders}
			bind:roots
			bind:checks
			bind:checking
		/>
	{:else if step === 3 && preview}
		<ArrProfilesStep {app} profiles={preview.quality_profiles} {choices} />
	{:else if step === 4 && preview}
		<ArrConfigStep
			{app}
			{url}
			{apiKey}
			indexers={preview.indexers}
			clients={preview.download_clients}
			bind:indexerPicks
			bind:clientPicks
			bind:applied
		/>
	{:else if step === 5}
		<ArrModeStep {app} bind:mode bind:importMode />
	{/if}

	<div class="flex items-center justify-between gap-3 border-t border-border pt-4">
		{#if step > 1}
			<button
				type="button"
				onclick={() => go(step - 1)}
				disabled={start.isPending}
				class={cn(BTN, "border border-border bg-bg-base text-fg-muted hover:border-border-strong hover:text-fg")}
			>
				<ChevronLeft size={14} aria-hidden="true" />
				{i18n.common_back()}
			</button>
		{:else}
			<span></span>
		{/if}

		{#if step < TOTAL}
			<button
				type="button"
				disabled={!canNext}
				onclick={() => go(step + 1)}
				class={cn(BTN, PRIMARY)}
			>
				{step === 4 && added === 0 ? i18n.common_skip() : i18n.common_next()}
				<ChevronRight size={14} aria-hidden="true" />
			</button>
		{:else}
			<button
				type="button"
				disabled={start.isPending || !preview}
				onclick={submit}
				class={cn(BTN, PRIMARY)}
			>
				<Play size={14} aria-hidden="true" />
				{start.isPending ? i18n.common_starting() : i18n.arr_start()}
			</button>
		{/if}
	</div>
</div>
