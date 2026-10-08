<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { goto } from "@roxi/routify";
	import { onMount } from "svelte";
	import { Play } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { importStartForm } from "@lib/schemas";
	import { hardcoverIssue } from "@lib/music-books-lookup";
	import type {
		ImportMode,
		ImportScan,
		ImportScanKind,
		ImportSource,
		ImportStartRequest,
		ImportTransferMode,
	} from "@lib/types";
	import TextField from "@components/forms/TextField.svelte";
	import Select from "@components/forms/Select.svelte";
	import RadioCards from "@components/forms/RadioCards.svelte";
	import ProviderKeyNotice from "@components/shared/ProviderKeyNotice.svelte";
	import ArrImportWizard from "./arr/ArrImportWizard.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Values = {
		source_path: string;
		kind: ImportScanKind;
		mode: ImportMode;
		import_mode: "" | ImportTransferMode;
	};

	type Props = { onCreated?: () => void };
	let { onCreated }: Props = $props();

	const qc = useQueryClient();

	// Which kind of import this is. It sits outside the folder form: a Radarr or
	// Sonarr migration is its own wizard, and the folder form below stays
	// exactly as it was.
	let source = $state<ImportSource>("filesystem");

	const SOURCES: { value: ImportSource; label: string; description: string }[] =
		[
			{
				value: "filesystem",
				label: i18n.imports_source_folder(),
				description: i18n.imports_source_folder_desc(),
			},
			{
				value: "radarr",
				label: "Radarr",
				description: i18n.imports_source_radarr_desc(),
			},
			{
				value: "sonarr",
				label: "Sonarr",
				description: i18n.imports_source_sonarr_desc(),
			},
		];

	// get(goto) inside the onSuccess callback throws "derived() expects stores
	// as input" — goto is a derived store and re-subscribing once the mutation
	// callback runs lands on a falsy fragment. Snapshot the navigate fn instead.
	// goto resolves route PATTERNS (`/imports/[id]`), not concrete
	// paths — passing `/imports/7` fails with "could not travel to 7".
	let navigate: (path: string, params?: Record<string, string>) => void =
		() => {};
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	// Starting a book scan with no Hardcover key answers 503 before anything is
	// queued. A key Hardcover refuses cannot fail the start: that surfaces on the
	// scan itself.
	let keyUnset = $state(false);

	const start = createMutation<ImportScan, Error, ImportStartRequest>(() => ({
		mutationFn: (body) =>
			api<ImportScan>("/library/imports", { method: "POST", body }),
		onSuccess: (scan) => {
			qc.invalidateQueries({ queryKey: ["imports"] });
			toast.ok(i18n.imports_scan_started());
			onCreated?.();
			navigate("/imports/[id]", { id: String(scan.id) });
		},
		onError: (err) => {
			if (hardcoverIssue(err) === "unset") keyUnset = true;
			else toast.err(errorText(err));
		},
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
			// Music and books are only ever adopted where they sit, whatever the
			// mode field still holds from another kind — it is left alone on a kind
			// change, since setting it would validate the untouched path too.
			const adopt = value.kind === "music" || value.kind === "book";
			const body: ImportStartRequest = {
				source_path: value.source_path,
				kind: value.kind,
				mode: adopt ? "in_place" : value.mode,
			};
			if (!adopt && value.mode === "rename" && value.import_mode) {
				body.import_mode = value.import_mode;
			}
			keyUnset = false;
			start.mutate(body);
		},
	}));

	// Mirrored rather than read off form.state: neither a top-level $derived nor
	// a template read of form.state.values re-runs when a field changes, so the
	// copy below would stay stuck on the movie wording and the transfer-mode
	// select would never appear.
	let kind = $state<ImportScanKind>("movie");
	// Where each kind's files usually wait, and what one entry in the review is.
	// Music and books have no rename step at import, so no rename wording.
	const PATH: Record<ImportScanKind, { placeholder: string; help: () => string; rename?: () => string }> = {
		movie: { placeholder: "/data/movies/incoming", help: i18n.imports_path_help, rename: i18n.imports_rename_desc_movie },
		series: { placeholder: "/data/tv/incoming", help: i18n.imports_shows_path_help, rename: i18n.imports_rename_desc_series },
		music: { placeholder: "/data/music/incoming", help: i18n.imports_albums_path_help },
		book: { placeholder: "/data/books/incoming", help: i18n.imports_books_path_help },
	};
	let path = $derived(PATH[kind]);
	let mode = $state<ImportMode>("in_place");
	// The backend adopts album folders and books in place and nothing else, so
	// those two kinds get the mode as a fact rather than a choice, and no
	// transfer mode. A book can still be renamed from its page afterwards.
	let adoptOnly = $derived(kind === "music" || kind === "book");

	const KINDS: { v: ImportScanKind; label: string; desc: string }[] = [
		{
			v: "movie",
			label: i18n.movies_label(),
			desc: i18n.imports_one_per_file(),
		},
		{
			v: "series",
			label: i18n.settings_series(),
			desc: i18n.imports_one_per_show(),
		},
		{
			v: "music",
			label: i18n.music_label(),
			desc: i18n.imports_one_per_album(),
		},
		{
			v: "book",
			label: i18n.books_label(),
			desc: i18n.imports_one_per_book(),
		},
	];

	const MODES: { v: ImportMode; label: string; desc: string }[] = $derived([
		{
			v: "in_place",
			label: i18n.imports_adopt_in_place(),
			desc: i18n.imports_adopt_help(),
		},
		{
			v: "rename",
			label: i18n.imports_import_rename(),
			desc: path.rename?.() ?? "",
		},
	]);

	const TRANSFER_MODES: { v: "" | ImportTransferMode; label: string }[] = [
		{ v: "", label: i18n.imports_mode_server_default() },
		{ v: "hardlink", label: i18n.imports_mode_hardlink() },
		{ v: "copy", label: i18n.imports_mode_copy() },
		{ v: "move", label: i18n.imports_mode_move() },
	];
</script>

<div class="space-y-5">
	<RadioCards
		legend={i18n.imports_source_label()}
		columns={3}
		name="import-source"
		value={source}
		onChange={(v) => (source = v)}
		options={SOURCES}
	/>

	{#if source === "filesystem"}
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
						legend={i18n.imports_media_type()}
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
						label={i18n.imports_source_path()}
						placeholder={path.placeholder}
						autocomplete="off"
						help={path.help()}
					/>
				{/snippet}
			</form.Field>

			{#if adoptOnly}
				<div>
					<p class="mb-2 text-sm font-medium text-fg-muted">{i18n.common_mode()}</p>
					<div class="flex flex-col gap-1.5 rounded-md border border-border bg-bg-card p-4">
						<span class="text-sm font-semibold text-fg">{i18n.imports_adopt_in_place()}</span>
						<span class="text-xs text-fg-muted">
							{kind === "book" ? i18n.imports_in_place_books() : i18n.imports_in_place_music()}
						</span>
					</div>
				</div>
			{:else}
				<form.Field name="mode">
					{#snippet children(field)}
						<RadioCards
							legend={i18n.common_mode()}
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
			{/if}

			{#if !adoptOnly && mode === "rename"}
				<form.Field name="import_mode">
					{#snippet children(field)}
						<div>
							<Select
								label={i18n.imports_transfer_mode()}
								value={field.state.value}
								options={TRANSFER_MODES.map((t) => ({
									value: t.v,
									label: t.label,
								}))}
								onChange={(v) => field.handleChange(v)}
							/>
							<p class="mt-1 text-xs text-fg-muted">
								{i18n.imports_overrides_global()}
							</p>
						</div>
					{/snippet}
				</form.Field>
			{/if}

			{#if keyUnset && kind === "book"}
				<ProviderKeyNotice reason="unset" onNavigate={onCreated} />
			{/if}

			<div class="flex justify-end">
				<button
					type="submit"
					disabled={!form.state.canSubmit || form.state.isSubmitting}
					class="inline-flex items-center gap-1.5 rounded-md bg-accent px-4 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
				>
					<Play size={14} aria-hidden="true" />
					{form.state.isSubmitting ? i18n.common_starting() : i18n.imports_start_scan()}
				</button>
			</div>
		</form>
	{:else}
		<!-- Keyed on the app: switching between Radarr and Sonarr starts over
		     rather than carrying one instance's preview into the other. -->
		{#key source}
			<ArrImportWizard app={source} {onCreated} />
		{/key}
	{/if}
</div>
