<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { TriangleAlert } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import type { FFmpegConfig, LibraryConfig } from "../../lib/types";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import FieldLock from "../../components/forms/FieldLock.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const qc = useQueryClient();

	const ffmpeg = createQuery<FFmpegConfig>(() => ({
		queryKey: ["config", "ffmpeg"],
		queryFn: () => api<FFmpegConfig>("/config/ffmpeg"),
	}));
	const library = createQuery<LibraryConfig>(() => ({
		queryKey: ["config", "library"],
		queryFn: () => api<LibraryConfig>("/config/library"),
	}));

	const saveFfmpeg = createMutation<FFmpegConfig, Error, Partial<FFmpegConfig>>(
		() => ({
			mutationFn: (body) =>
				api<FFmpegConfig>("/config/ffmpeg", { method: "PATCH", body }),
			onSuccess: (resp) => {
				qc.setQueryData(["config", "ffmpeg"], resp);
				// enabled feeds SystemInfo.ffmpeg_warn, which is what draws the notice
				// on Settings -> General and turns the top-bar health pill amber. That
				// lives under a different key, and the top bar never remounts, so
				// without this it keeps answering with the pre-toggle state.
				qc.invalidateQueries({ queryKey: ["system", "info"] });
				toast.ok(i18n.probe_saved());
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);
	const saveLibrary = createMutation<
		LibraryConfig,
		Error,
		Partial<LibraryConfig>
	>(() => ({
		mutationFn: (body) =>
			api<LibraryConfig>("/config/library", { method: "PATCH", body }),
		onSuccess: (resp) => {
			qc.setQueryData(["config", "library"], resp);
			toast.ok(i18n.probe_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// Every control saves on its own, as on /settings/series — so there is no
	// form and no Save button, and the two text inputs commit on blur rather than
	// on each keystroke. That is also why they are plain inputs: TextField needs a
	// TanStack field API, and standing a form up for two fields would put a second
	// save model on one page.
	let pathDraft = $state<string | null>(null);
	let path = $derived(pathDraft ?? ffmpeg.data?.path ?? "");

	function commitPath() {
		const next = (pathDraft ?? "").trim();
		pathDraft = null;
		if (!ffmpeg.data || next === ffmpeg.data.path) return;
		saveFfmpeg.mutate({ path: next });
	}

	// Stored as a ratio (0.5), said out loud as a percentage (50%). The field is
	// the one place the two representations meet.
	let ratioDraft = $state<string | null>(null);
	let ratioPct = $derived(
		ratioDraft ??
			String(Math.round((library.data?.probe?.min_duration_ratio ?? 0.5) * 100)),
	);

	function commitRatio() {
		const raw = ratioDraft;
		ratioDraft = null;
		if (raw === null || !library.data) return;
		const pct = Number(raw);
		if (!Number.isFinite(pct)) return;
		const ratio = Math.min(100, Math.max(1, Math.round(pct))) / 100;
		if (ratio === (library.data.probe?.min_duration_ratio ?? 0.5)) return;
		saveLibrary.mutate({
			probe: { ...library.data.probe, min_duration_ratio: ratio },
		});
	}

	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

	let pending = $derived(ffmpeg.isPending || library.isPending);
	let failed = $derived(ffmpeg.isError || library.isError);
	// The binary is missing only when we are asking for it at all.
	let missing = $derived(
		Boolean(ffmpeg.data?.enabled) && ffmpeg.data?.found === false,
	);
	// setQueryData in saveFfmpeg's onSuccess keeps this current off the PATCH
	// response itself, so the notice appears right after a path save — not
	// only on the next page load.
	let restartRequired = $derived(ffmpeg.data?.restart_required ?? false);
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">
			{i18n.settings_media_probe()}
		</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{i18n.settings_media_probe_intro()}
		</p>
	</header>

	{#if pending}
		<p class="mt-6 text-sm text-fg-subtle">{i18n.common_loading()}</p>
	{:else if failed}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({
				reason: errorText(ffmpeg.error ?? library.error),
			})}
		</p>
	{:else if ffmpeg.data && library.data}
		{#if restartRequired}
			<div
				class="mt-6 flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
			>
				<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<div>
					<p class="font-medium">{i18n.settings_restart_required()}</p>
					<p class="mt-0.5 text-status-wanted/80">
						{i18n.settings_changes_after_restart()}
					</p>
				</div>
			</div>
		{/if}

		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.probe_ffmpeg()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.probe_ffmpeg_help()}
			</p>

			<div class="mt-4">
				<Checkbox
					checked={ffmpeg.data.enabled}
					disabled={config.readOnly || saveFfmpeg.isPending}
					onChange={(v) => saveFfmpeg.mutate({ enabled: v })}
					label={i18n.probe_enable()}
					description={i18n.probe_enable_help()}
				/>
			</div>

			<label class="mt-4 block">
				<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.probe_path()}
					<FieldLock locked={config.readOnly} />
				</span>
				<input
					type="text"
					spellcheck="false"
					autocapitalize="off"
					autocomplete="off"
					readonly={config.readOnly}
					value={path}
					placeholder="/usr/local/bin"
					oninput={(e) => (pathDraft = (e.currentTarget as HTMLInputElement).value)}
					onblur={commitPath}
					onkeydown={(e) => {
						if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
					}}
					class="{inputClass} font-mono"
				/>
				<p class="mt-1 text-xs text-fg-muted">{i18n.probe_path_help()}</p>
				{#if missing}
					<p class="mt-2 flex gap-2 text-xs leading-relaxed text-status-wanted">
						<TriangleAlert size={14} class="mt-px shrink-0" aria-hidden="true" />
						<span>{i18n.probe_not_found()}</span>
					</p>
				{/if}
			</label>
		</section>

		<section class="mt-4 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-semibold text-fg">{i18n.probe_verification()}</h2>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
				{i18n.probe_verification_help()}
			</p>

			<div class="mt-4">
				<Checkbox
					checked={library.data.probe?.always_ask ?? false}
					disabled={config.readOnly || saveLibrary.isPending}
					onChange={(v) =>
						saveLibrary.mutate({
							probe: { ...library.data?.probe, always_ask: v },
						})}
					label={i18n.probe_always_ask()}
					description={i18n.probe_always_ask_help()}
				/>
			</div>

			<label class="mt-4 block">
				<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.probe_min_duration()}
					<FieldLock locked={config.readOnly} />
				</span>
				<span class="relative block w-28">
					<input
						type="number"
						min="1"
						max="100"
						inputmode="numeric"
						readonly={config.readOnly}
						value={ratioPct}
						oninput={(e) =>
							(ratioDraft = (e.currentTarget as HTMLInputElement).value)}
						onblur={commitRatio}
						onkeydown={(e) => {
							if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
						}}
						class="{inputClass} pr-8 font-mono tabular"
					/>
					<span
						class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 font-mono text-xs text-fg-faint"
						aria-hidden="true"
					>
						%
					</span>
				</span>
				<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-muted">
					{i18n.probe_min_duration_help()}
				</p>
			</label>
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
