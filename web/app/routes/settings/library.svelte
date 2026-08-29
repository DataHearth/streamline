<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { FolderInput } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type {
		DownloadConfig,
		ImportTransferMode,
		LibraryConfig,
	} from "../../lib/types";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import Select from "../../components/forms/Select.svelte";
	import FieldLock from "../../components/forms/FieldLock.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const library = createQuery<LibraryConfig>(() => ({
		queryKey: ["config", "library"],
		queryFn: () => api<LibraryConfig>("/config/library"),
	}));
	const download = createQuery<DownloadConfig>(() => ({
		queryKey: ["config", "download"],
		queryFn: () => api<DownloadConfig>("/config/download"),
	}));

	// Every control saves on its own, as on /settings/media-probe — so there is
	// no form and no Save button, and the text inputs commit on blur rather than
	// on each keystroke. Two endpoints back this page and a single form could
	// not straddle them.
	const saveLibrary = createMutation<
		LibraryConfig,
		Error,
		Partial<LibraryConfig>
	>(() => ({
		mutationFn: (body) =>
			api<LibraryConfig>("/config/library", { method: "PATCH", body }),
		onSuccess: (resp) => {
			qc.setQueryData(["config", "library"], resp);
			toast.ok(i18n.library_settings_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const saveDownload = createMutation<
		DownloadConfig,
		Error,
		Partial<DownloadConfig>
	>(() => ({
		mutationFn: (body) =>
			api<DownloadConfig>("/config/download", { method: "PATCH", body }),
		onSuccess: (resp) => {
			qc.setQueryData(["config", "download"], resp);
			toast.ok(i18n.library_settings_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// One draft per field, keyed by the field name. Null means "not being
	// edited", so the input falls back to the stored value and a refetch that
	// lands mid-typing cannot overwrite what is in the box.
	let drafts = $state<Record<string, string>>({});

	function draftOf(key: string, stored: string | number) {
		return drafts[key] ?? String(stored);
	}

	function commitText(
		key: string,
		stored: string,
		save: (v: string) => void,
	) {
		const next = (drafts[key] ?? "").trim();
		delete drafts[key];
		if (next === "" || next === stored) return;
		save(next);
	}

	// Bounded counters: the API refuses anything outside its range with a 422,
	// so clamping here is about not making the round trip, not about trust.
	function commitNumber(
		key: string,
		stored: number,
		max: number,
		save: (v: number) => void,
	) {
		const raw = drafts[key];
		delete drafts[key];
		if (raw === undefined) return;
		const n = Math.round(Number(raw));
		if (!Number.isFinite(n)) return;
		const clamped = Math.min(max, Math.max(1, n));
		if (clamped === stored) return;
		save(clamped);
	}

	// allowed_download_roots is a list, edited one path per line — the shape an
	// operator already has when copying mount points out of a compose file.
	function commitRoots(stored: string[]) {
		const raw = drafts.roots;
		delete drafts.roots;
		if (raw === undefined) return;
		const next = raw
			.split("\n")
			.map((s) => s.trim())
			.filter(Boolean);
		if (next.join("\n") === stored.join("\n")) return;
		saveLibrary.mutate({ allowed_download_roots: next });
	}

	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

	let pending = $derived(library.isPending || download.isPending);
	let failed = $derived(library.isError || download.isError);
	let roots = $derived(library.data?.allowed_download_roots ?? []);
</script>

{#snippet field(
	key: string,
	label: string,
	help: string,
	value: string,
	commit: () => void,
	opts: { mono?: boolean; numeric?: boolean; width?: string } = {},
)}
	<label class="block">
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
			{label}
			<FieldLock locked={config.readOnly} />
		</span>
		<span class="block {opts.width ?? ''}">
			<input
				type={opts.numeric ? "number" : "text"}
				spellcheck="false"
				autocapitalize="off"
				autocomplete="off"
				inputmode={opts.numeric ? "numeric" : undefined}
				readonly={config.readOnly}
				{value}
				oninput={(e) =>
					(drafts[key] = (e.currentTarget as HTMLInputElement).value)}
				onblur={commit}
				onkeydown={(e) => {
					if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
				}}
				class="{inputClass} {opts.mono || opts.numeric ? 'font-mono' : ''}"
			/>
		</span>
		<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">{help}</p>
	</label>
{/snippet}

{#snippet readonlyPath(label: string, value: string)}
	<div class="min-w-0">
		<p class="text-xs font-medium text-fg-muted">{label}</p>
		<p class="truncate font-mono text-sm text-fg" title={value}>{value}</p>
	</div>
{/snippet}

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.settings_library()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">{i18n.settings_library_intro()}</p>
	</header>

	{#if pending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if failed}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({
				reason: errorText(library.error ?? download.error),
			})}
		</p>
	{:else if library.data && download.data}
		{@const lib = library.data}
		{@const dl = download.data}

		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.library_roots()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.library_roots_help()}
			</p>
			<div class="mt-4 grid gap-3 sm:grid-cols-3">
				{@render readonlyPath(i18n.library_movie_path(), lib.movie_path ?? "")}
				{@render readonlyPath(i18n.library_series_path(), lib.series_path ?? "")}
				{@render readonlyPath(
					i18n.library_download_path(),
					lib.download_path ?? "",
				)}
			</div>
			<a
				href="/settings/advanced"
				class="touch-hit mt-3 inline-flex items-center gap-1.5 text-xs font-medium text-accent hover:underline"
			>
				<FolderInput size={13} aria-hidden="true" />
				{i18n.library_move_a_root()}
			</a>
		</section>

		<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
			<div>
				<h2 class="text-sm font-semibold text-fg">{i18n.library_naming()}</h2>
				<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
					{i18n.library_naming_help()}
				</p>
			</div>
			{@render field(
				"movie_naming",
				i18n.library_movie_naming(),
				i18n.library_movie_naming_help(),
				draftOf("movie_naming", lib.movie_naming),
				() =>
					commitText("movie_naming", lib.movie_naming, (v) =>
						saveLibrary.mutate({ movie_naming: v }),
					),
				{ mono: true },
			)}
			{@render field(
				"series_naming",
				i18n.library_series_naming(),
				i18n.library_series_naming_help(),
				draftOf("series_naming", lib.series_naming),
				() =>
					commitText("series_naming", lib.series_naming, (v) =>
						saveLibrary.mutate({ series_naming: v }),
					),
				{ mono: true },
			)}
		</section>

		<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
			<div>
				<h2 class="text-sm font-semibold text-fg">{i18n.library_importing()}</h2>
				<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
					{i18n.library_importing_help()}
				</p>
			</div>

			<div class="max-w-xs">
				<Select
					label={i18n.library_import_mode()}
					value={lib.import_mode}
					options={[
						{
							value: "hardlink",
							label: i18n.library_import_hardlink(),
							hint: i18n.library_import_hardlink_hint(),
						},
						{
							value: "copy",
							label: i18n.library_import_copy(),
							hint: i18n.library_import_copy_hint(),
						},
						{
							value: "move",
							label: i18n.library_import_move(),
							hint: i18n.library_import_move_hint(),
						},
					]}
					onChange={(v) => saveLibrary.mutate({ import_mode: v as ImportTransferMode })}
				/>
			</div>

			<Checkbox
				checked={lib.keep_torrent_seeding}
				disabled={saveLibrary.isPending}
				onChange={(v) => saveLibrary.mutate({ keep_torrent_seeding: v })}
				label={i18n.library_keep_seeding()}
				description={i18n.library_keep_seeding_help()}
			/>

			{@render field(
				"import_max_attempts",
				i18n.library_import_attempts(),
				i18n.library_import_attempts_help(),
				draftOf("import_max_attempts", lib.import_max_attempts),
				() =>
					commitNumber(
						"import_max_attempts",
						lib.import_max_attempts,
						255,
						(v) => saveLibrary.mutate({ import_max_attempts: v }),
					),
				{ numeric: true, width: "w-28" },
			)}

			<label class="block">
				<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.library_allowed_roots()}
					<FieldLock locked={config.readOnly} />
				</span>
				<textarea
					rows="3"
					spellcheck="false"
					readonly={config.readOnly}
					value={drafts.roots ?? roots.join("\n")}
					placeholder="/downloads"
					oninput={(e) =>
						(drafts.roots = (e.currentTarget as HTMLTextAreaElement).value)}
					onblur={() => commitRoots(roots)}
					class="{inputClass} font-mono"
				></textarea>
				<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">
					{i18n.library_allowed_roots_help()}
				</p>
			</label>
		</section>

		<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
			<div>
				<h2 class="text-sm font-semibold text-fg">{i18n.library_grabbing()}</h2>
				<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
					{i18n.library_grabbing_help()}
				</p>
			</div>

			<Checkbox
				checked={dl.selective_files}
				disabled={saveDownload.isPending}
				onChange={(v) => saveDownload.mutate({ selective_files: v })}
				label={i18n.library_selective_files()}
				description={i18n.library_selective_files_help()}
			/>

			{#if dl.selective_files}
				{@render field(
					"selection_grace",
					i18n.library_selection_grace(),
					i18n.library_selection_grace_help(),
					draftOf("selection_grace", dl.selection_grace),
					() =>
						commitText("selection_grace", dl.selection_grace, (v) =>
							saveDownload.mutate({ selection_grace: v }),
						),
					{ mono: true, width: "w-32" },
				)}
			{/if}

			{@render field(
				"no_match_cooldown",
				i18n.library_no_match_cooldown(),
				i18n.library_no_match_cooldown_help(),
				draftOf("no_match_cooldown", lib.no_match_cooldown),
				() =>
					commitText("no_match_cooldown", lib.no_match_cooldown, (v) =>
						saveLibrary.mutate({ no_match_cooldown: v }),
					),
				{ mono: true, width: "w-32" },
			)}

			{@render field(
				"max_grab_failures",
				i18n.library_max_grab_failures(),
				i18n.library_max_grab_failures_help(),
				draftOf("max_grab_failures", lib.max_grab_failures),
				() =>
					commitNumber("max_grab_failures", lib.max_grab_failures, 255, (v) =>
						saveLibrary.mutate({ max_grab_failures: v }),
					),
				{ numeric: true, width: "w-28" },
			)}
		</section>

		<section class="mt-4 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.library_maintenance()}</h2>
			<p class="mt-0.5 mb-4 text-xs leading-relaxed text-fg-subtle">
				{i18n.library_maintenance_help()}
			</p>
			{@render field(
				"drift_grace_ticks",
				i18n.library_drift_grace(),
				i18n.library_drift_grace_help(),
				draftOf("drift_grace_ticks", lib.drift_grace_ticks),
				() =>
					commitNumber("drift_grace_ticks", lib.drift_grace_ticks, 20, (v) =>
						saveLibrary.mutate({ drift_grace_ticks: v }),
					),
				{ numeric: true, width: "w-28" },
			)}
		</section>
	{/if}
</div>

<style>
	/* Match TextField: drop the native spin buttons. */
	input[type="number"]::-webkit-inner-spin-button,
	input[type="number"]::-webkit-outer-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	input[type="number"] {
		-moz-appearance: textfield;
		appearance: textfield;
	}
</style>
