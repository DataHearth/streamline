<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { goto } from "@roxi/routify";
	import { onMount } from "svelte";
	import { Play } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { importStartForm } from "../../lib/schemas";
	import type {
		ImportMode,
		ImportScan,
		ImportScanKind,
		ImportStartRequest,
		ImportTransferMode,
	} from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import Select from "../forms/Select.svelte";
	import RadioCards from "../forms/RadioCards.svelte";

	type Values = {
		source_path: string;
		kind: ImportScanKind;
		mode: ImportMode;
		import_mode: "" | ImportTransferMode;
	};

	type Props = { onCreated?: () => void };
	let { onCreated }: Props = $props();

	const qc = useQueryClient();

	// get(goto) inside the onSuccess callback throws "derived() expects stores
	// as input" — goto is a derived store and re-subscribing once the mutation
	// callback runs lands on a falsy fragment. Snapshot the navigate fn instead.
	// goto resolves route PATTERNS (`/library/imports/[id]`), not concrete
	// paths — passing `/library/imports/7` fails with "could not travel to 7".
	let navigate: (path: string, params?: Record<string, string>) => void =
		() => {};
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	const start = createMutation<ImportScan, Error, ImportStartRequest>(() => ({
		mutationFn: (body) =>
			api<ImportScan>("/library/imports", { method: "POST", body }),
		onSuccess: (scan) => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok("Scan started");
			onCreated?.();
			navigate("/library/imports/[id]", { id: String(scan.id) });
		},
		onError: (err) => toast.err(err.message),
	}));

	const form = createForm(() => ({
		defaultValues: {
			source_path: "",
			kind: "movie" as ImportScanKind,
			mode: "in_place" as ImportMode,
			import_mode: "" as Values["import_mode"],
		},
		validators: { onChange: importStartForm },
		onSubmit: ({ value }) => {
			const body: ImportStartRequest = {
				source_path: value.source_path,
				kind: value.kind,
				mode: value.mode,
			};
			if (value.mode === "rename" && value.import_mode) {
				body.import_mode = value.import_mode;
			}
			start.mutate(body);
		},
	}));

	// Mirrored rather than read off form.state: neither a top-level $derived nor
	// a template read of form.state.values re-runs when a field changes, so the
	// copy below would stay stuck on the movie wording and the transfer-mode
	// select would never appear.
	let kind = $state<ImportScanKind>("movie");
	let isSeries = $derived(kind === "series");
	let mode = $state<ImportMode>("in_place");

	const KINDS: { v: ImportScanKind; label: string; desc: string }[] = [
		{
			v: "movie",
			label: "Movies",
			desc: "One entry per file, matched against TMDB.",
		},
		{
			v: "series",
			label: "Series",
			desc: "One entry per show folder, matched against TVDB.",
		},
	];

	const MODES: { v: ImportMode; label: string; desc: string }[] = $derived([
		{
			v: "in_place",
			label: "Adopt in place",
			desc: "Files already inside your library — keep them where they are.",
		},
		{
			v: "rename",
			label: "Import & rename",
			desc: `Files outside the library — copy/move into the configured ${
				isSeries ? "series" : "movie"
			} path.`,
		},
	]);

	const TRANSFER_MODES: { v: "" | ImportTransferMode; label: string }[] = [
		{ v: "", label: "Use server default (library.import_mode)" },
		{ v: "hardlink", label: "Hardlink — same filesystem, instant, no extra disk" },
		{ v: "copy", label: "Copy — leaves original intact, uses double the disk" },
		{ v: "move", label: "Move — destructive, frees source disk" },
	];
</script>

<form
	class="space-y-5"
	onsubmit={(e) => {
		e.preventDefault();
		form.handleSubmit();
	}}
>
	<form.Field name="kind">
		{#snippet children(field)}
			<RadioCards
				legend="Media type"
				columns={2}
				name={field.name}
				value={field.state.value}
				onChange={(v) => {
					field.handleChange(v);
					kind = v;
				}}
				options={KINDS.map((k) => ({
					value: k.v,
					label: k.label,
					description: k.desc,
				}))}
			/>
		{/snippet}
	</form.Field>

	<form.Field name="source_path">
		{#snippet children(field)}
			<TextField
				{field}
				label="Source path"
				placeholder={isSeries ? "/data/tv/incoming" : "/data/movies/incoming"}
				autocomplete="off"
				help={isSeries
					? "Absolute path on the server holding one folder per show, e.g. /data/tv/incoming."
					: "Absolute path on the server, e.g. /data/movies/incoming."}
			/>
		{/snippet}
	</form.Field>

	<form.Field name="mode">
		{#snippet children(field)}
			<RadioCards
				legend="Mode"
				columns={2}
				name={field.name}
				value={field.state.value}
				onChange={(v) => {
					field.handleChange(v);
					mode = v;
				}}
				options={MODES.map((m) => ({
					value: m.v,
					label: m.label,
					description: m.desc,
				}))}
			/>
		{/snippet}
	</form.Field>

	{#if mode === "rename"}
		<form.Field name="import_mode">
			{#snippet children(field)}
				<div>
					<Select
						label="Transfer mode"
						value={field.state.value}
						options={TRANSFER_MODES.map((t) => ({
							value: t.v,
							label: t.label,
						}))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						Overrides the global setting for this scan only.
					</p>
				</div>
			{/snippet}
		</form.Field>
	{/if}

	<div class="flex justify-end">
		<button
			type="submit"
			disabled={!form.state.canSubmit || form.state.isSubmitting}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-4 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			<Play size={14} aria-hidden="true" />
			{form.state.isSubmitting ? "Starting…" : "Start scan"}
		</button>
	</div>
</form>
