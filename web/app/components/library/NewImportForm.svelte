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
		ImportStartRequest,
		ImportTransferMode,
	} from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import Select from "../forms/Select.svelte";
	import RadioCards from "../forms/RadioCards.svelte";

	type Values = {
		source_path: string;
		mode: ImportMode;
		import_mode: "" | ImportTransferMode;
	};

	type Props = { onCreated?: () => void };
	let { onCreated }: Props = $props();

	const qc = useQueryClient();

	// get(goto) inside the onSuccess callback throws "derived() expects stores
	// as input" — goto is a derived store and re-subscribing once the mutation
	// callback runs lands on a falsy fragment. Snapshot the navigate fn instead.
	let navigate: (path: string) => void = () => {};
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	const start = createMutation<ImportScan, Error, ImportStartRequest>(() => ({
		mutationFn: (body) =>
			api<ImportScan>("/library/imports", { method: "POST", body }),
		onSuccess: (scan) => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok("Scan started");
			onCreated?.();
			navigate(`/library/imports/${scan.id}`);
		},
		onError: (err) => toast.err(err.message),
	}));

	const form = createForm(() => ({
		defaultValues: {
			source_path: "",
			mode: "in_place" as ImportMode,
			import_mode: "" as Values["import_mode"],
		},
		validators: { onChange: importStartForm },
		onSubmit: ({ value }) => {
			const body: ImportStartRequest = {
				source_path: value.source_path,
				mode: value.mode,
			};
			if (value.mode === "rename" && value.import_mode) {
				body.import_mode = value.import_mode;
			}
			start.mutate(body);
		},
	}));

	const MODES: { v: ImportMode; label: string; desc: string }[] = [
		{
			v: "in_place",
			label: "Adopt in place",
			desc: "Files already inside your library — keep them where they are.",
		},
		{
			v: "rename",
			label: "Import & rename",
			desc: "Files outside the library — copy/move into the configured movie path.",
		},
	];

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
	<form.Field name="source_path">
		{#snippet children(field)}
			<TextField
				{field}
				label="Source path"
				placeholder="/data/movies/incoming"
				autocomplete="off"
				help="Absolute path on the server, e.g. /data/movies/incoming."
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
				onChange={(v) => field.handleChange(v)}
				options={MODES.map((m) => ({
					value: m.v,
					label: m.label,
					description: m.desc,
				}))}
			/>
		{/snippet}
	</form.Field>

	{#if form.state.values.mode === "rename"}
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
