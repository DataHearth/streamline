<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { ArrowLeft, CircleAlert, KeyRound, Puzzle } from "@lucide/svelte";
	import { api, ApiError, errorText } from "@lib/api";
	import { config } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import {
		ACCOUNT_EXTENSIONS_KEY,
		EXTENSIONS_KEY,
		EXTENSIONS_PATH,
		extensionYaml,
		grantsOf,
		shaProblem,
		shortSha,
		sourceProblem,
		type Extension,
		type ExtensionInspect,
		type ExtensionSave,
	} from "@lib/extensions";
	import ConfigFormShell from "@components/modals/ConfigFormShell.svelte";
	import SourceFields from "./SourceFields.svelte";
	import ExtensionPermissions from "./ExtensionPermissions.svelte";
	import ExtensionYaml from "./ExtensionYaml.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Adding an extension, in two steps and no catalogue. The admin pastes the
	// source and checksum from the release notes; Inspect reads the module and
	// saves nothing; the review lists what it asks for, and confirming saves
	// the entry with exactly those grants — there is nothing optional to untick.
	//
	// Read-only instance: Inspect still runs (it writes nothing), and the
	// review ends on the entry as YAML instead of a save.
	let {
		open,
		onClose,
		onEditExisting,
	}: {
		open: boolean;
		onClose: () => void;
		// The name is taken: the way forward is the installed one's Edit.
		onEditExisting: (name: string) => void;
	} = $props();

	type Problem = { kind: "unreadable" | "taken" | "other"; text: string; name?: string };

	let step = $state<"source" | "review">("source");
	let source = $state("");
	let sha256 = $state("");
	let tried = $state(false);
	let found = $state<ExtensionInspect | null>(null);
	let problem = $state<Problem | null>(null);

	// Fresh every time it opens.
	let wasOpen = false;
	$effect.pre(() => {
		if (open && !wasOpen) {
			step = "source";
			source = "";
			sha256 = "";
			tried = false;
			found = null;
			problem = null;
		}
		wasOpen = open;
	});

	const qc = useQueryClient();
	let body = $derived({
		source: source.trim(),
		...(sha256.trim() ? { sha256: sha256.trim().toLowerCase() } : {}),
	});

	function describe(err: Error): Problem {
		if (err instanceof ApiError && err.body?.code === "extension_unreadable") {
			return { kind: "unreadable", text: err.message };
		}
		if (err instanceof ApiError && err.status === 409) {
			const name = (err.body as { name?: string } | null)?.name ?? found?.name ?? "";
			return { kind: "taken", name, text: i18n.extensions_name_taken({ name }) };
		}
		return { kind: "other", text: errorText(err) };
	}

	const inspect = createMutation<ExtensionInspect, Error, void>(() => ({
		mutationFn: () => api<ExtensionInspect>(`${EXTENSIONS_PATH}/inspect`, { method: "POST", body }),
		onSuccess: (r) => {
			found = r;
			problem = null;
			step = "review";
		},
		onError: (err) => (problem = describe(err)),
	}));

	const add = createMutation<Extension, Error, void>(() => ({
		mutationFn: () => {
			if (!found) throw new Error("no module was read");
			const save: ExtensionSave = { name: found.name, ...body, enabled: true, grants: grantsOf(found.requested) };
			return api<Extension>(EXTENSIONS_PATH, { method: "POST", body: save });
		},
		onSuccess: (x) => {
			qc.invalidateQueries({ queryKey: EXTENSIONS_KEY });
			qc.invalidateQueries({ queryKey: ACCOUNT_EXTENSIONS_KEY });
			toast.ok(i18n.extensions_added({ name: x.name }));
			onClose();
		},
		// Taken or unreadable since the inspect (another admin, a replaced
		// file): back to the source, where both are explained.
		onError: (err) => {
			const p = describe(err);
			if (p.kind === "other") return toast.err(p.text);
			problem = p;
			step = "source";
		},
	}));

	function submit() {
		if (step === "source") {
			tried = true;
			if (sourceProblem(source) || shaProblem(sha256, source)) return;
			problem = null;
			inspect.mutate();
		} else if (!config.readOnly) {
			add.mutate();
		}
	}

	// The step swaps the body out from under whatever had focus — on a
	// read-only instance the footer's button goes too — so focus follows the
	// step: the review on the way in, the source on the way back.
	let reviewEl = $state<HTMLElement | null>(null);
	$effect(() => {
		if (step === "review" && reviewEl) reviewEl.focus({ preventScroll: true });
		else if (step === "source" && found) document.getElementById("ext-add-source")?.focus();
	});

	let yaml = $derived(
		found ? extensionYaml({ name: found.name, ...body, enabled: true, grants: grantsOf(found.requested) }) : "",
	);
	let pending = $derived(inspect.isPending || add.isPending);
</script>

<ConfigFormShell
	{open}
	title={i18n.extensions_add()}
	size="xl"
	formId="extension-add-form"
	submitLabel={step === "source"
		? inspect.isPending
			? i18n.extensions_inspecting()
			: i18n.extensions_inspect()
		: add.isPending
			? i18n.common_saving()
			: i18n.extensions_grant_add()}
	submitDisabled={pending}
	showSubmit={step === "source" || undefined}
	{onClose}
>
	<form
		id="extension-add-form"
		novalidate
		onsubmit={(e) => {
			e.preventDefault();
			submit();
		}}
	>
		{#if step === "source" || !found}
			<p class="mb-4 text-sm leading-relaxed text-fg-muted">{i18n.extensions_add_lead()}</p>
			<SourceFields bind:source bind:sha256 showErrors={tried} idPrefix="ext-add" />

			{#if problem?.kind === "taken"}
				<div class="mt-4 flex items-start gap-2.5 rounded-md border border-status-wanted/30 bg-status-wanted/[0.07] p-3">
					<CircleAlert size={15} class="mt-0.5 shrink-0 text-status-wanted" aria-hidden="true" />
					<div class="min-w-0 flex-1">
						<p class="text-sm text-fg">{problem.text}</p>
						<button
							type="button"
							onclick={() => onEditExisting(problem?.name ?? "")}
							class="touch-hit mt-1 text-xs font-medium text-accent-text hover:underline"
						>
							{i18n.extensions_edit_existing({ name: problem.name ?? "" })}
						</button>
					</div>
				</div>
			{:else if problem}
				<div role="alert" class="mt-4 flex items-start gap-2.5 rounded-md border border-status-failed/30 bg-status-failed/[0.07] p-3">
					<CircleAlert size={15} class="mt-0.5 shrink-0 text-status-failed" aria-hidden="true" />
					<div class="min-w-0 flex-1">
						<p class="text-sm font-medium text-fg">{i18n.extensions_unreadable_title()}</p>
						<p class="mt-1 break-words font-mono text-[12px] leading-relaxed text-fg-muted">{problem.text}</p>
					</div>
				</div>
			{/if}

			<p class="mt-4 text-xs leading-relaxed text-fg-subtle">{i18n.extensions_inspect_help()}</p>
		{:else}
			<div bind:this={reviewEl} tabindex="-1" class="outline-none">
			<!-- Phones put Change source under the name, so the source and
			     checksum keep the card's width. -->
			<div class="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3 gap-y-1.5 rounded-lg border border-border bg-bg-card/40 p-3 sm:grid-cols-[auto_minmax(0,1fr)_auto]">
				<div class="row-span-2 grid h-10 w-10 place-items-center rounded-md bg-bg-card text-fg-muted sm:row-span-1" aria-hidden="true">
					<Puzzle size={19} />
				</div>
				<div class="min-w-0">
					<p class="flex flex-wrap items-baseline gap-x-2">
						<span class="text-[15px] font-semibold text-fg">{found.name}</span>
						<span class="font-mono text-xs text-fg-subtle">v{found.version}</span>
					</p>
					<p class="mt-0.5 truncate font-mono text-[11.5px] text-fg-subtle" title={body.source}>{body.source}</p>
					{#if body.sha256}
						<p class="truncate font-mono text-[11px] text-fg-faint" title={body.sha256}>sha256 {shortSha(body.sha256)}</p>
					{/if}
				</div>
				<button
					type="button"
					onclick={() => (step = "source")}
					class="touch-hit col-start-2 inline-flex items-center gap-1 justify-self-start rounded text-xs font-medium text-accent-text hover:underline sm:col-start-3 sm:justify-self-end sm:px-1"
				>
					<ArrowLeft size={12} aria-hidden="true" />
					{i18n.extensions_change_source()}
				</button>
			</div>

			<p class="mt-5 text-sm font-medium text-fg">{i18n.extensions_review_lead()}</p>
			<ExtensionPermissions permissions={found.requested} class="mt-3" />

			<div class="mt-5 flex flex-col gap-2 border-t border-border pt-4">
				{#if found.card}
					<p class="flex items-start gap-2 text-[13px] leading-relaxed text-fg-muted">
						<KeyRound size={14} class="mt-[3px] shrink-0 text-fg-subtle" aria-hidden="true" />
						<span>{i18n.extensions_review_card({ title: found.card.title })}</span>
					</p>
				{/if}
				<p class="text-xs leading-relaxed text-fg-subtle">{i18n.extensions_review_all()}</p>
			</div>

			{#if config.readOnly}
				<div class="mt-5">
					<ExtensionYaml {yaml} lead={i18n.extensions_yaml_add()} />
				</div>
			{/if}
			</div>
		{/if}
	</form>
</ConfigFormShell>
