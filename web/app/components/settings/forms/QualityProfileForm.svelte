<script lang="ts" module>
	import type {
		QualityProfileFormatScore,
		Resolution,
		TranscodeAudioCodec,
		TranscodePreset,
		TranscodeTargetCodec,
		TranscodeTargetContainer,
	} from "../../../lib/types";

	// The policy is flat and always present in the form, gated by
	// transcode_enabled. The page assembles the API's optional `transcode` from
	// the two — a half-filled policy is not a thing the API accepts.
	export type TranscodeFormValues = {
		if: {
			video_codecs: string[];
			containers: string[];
			max_video_bitrate: string;
		};
		to: {
			container: TranscodeTargetContainer;
			video_codec: TranscodeTargetCodec;
			crf: number;
			preset: TranscodePreset;
			audio_codec: TranscodeAudioCodec;
			audio_passthrough: string[];
		};
	};

	export const TRANSCODE_DEFAULTS: TranscodeFormValues = {
		if: { video_codecs: [], containers: [], max_video_bitrate: "" },
		to: {
			container: "mkv",
			video_codec: "hevc",
			crf: 0,
			preset: "medium",
			audio_codec: "aac",
			audio_passthrough: [],
		},
	};

	export type QualityProfileValues = {
		name: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		upgrade_allowed: boolean;
		allowed_codecs: string[];
		formats: QualityProfileFormatScore[];
		min_score: number;
		upgrade_until_score: number;
		transcode_enabled: boolean;
		transcode: TranscodeFormValues;
	};

	export type ProfilePreset = {
		label: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		// Empty means any codec — the same thing an empty chip row means.
		allowed_codecs: readonly string[];
		formats: readonly { readonly name: string; readonly score: number }[];
		min_score: number;
		upgrade_until_score: number;
	};

	// Starting points, not policy: applying one fills the create form and the
	// operator edits from there. Every name here is a built-in format, so a
	// preset saves against a fresh install with no custom formats defined —
	// which is also why none of them scores a group blocklist or a junk-source
	// rule: those are the operator's own custom formats, not ours to assume.
	// Codec values are ffprobe's, matching lib/media-info VIDEO_CODECS.
	export const PROFILE_PRESETS = [
		{
			label: "Quality first",
			preferred_resolution: "2160p",
			min_resolution: "1080p",
			allowed_codecs: [],
			formats: [
				{ name: "remux", score: 200 },
				{ name: "hdr", score: 100 },
			],
			min_score: 0,
			upgrade_until_score: 300,
		},
		{
			label: "Space saver",
			preferred_resolution: "1080p",
			min_resolution: "720p",
			allowed_codecs: ["hevc", "av1"],
			formats: [
				{ name: "x265", score: 100 },
				{ name: "av1", score: 80 },
				{ name: "remux", score: -100 },
			],
			min_score: 0,
			upgrade_until_score: 100,
		},
		{
			label: "x265 only",
			preferred_resolution: "1080p",
			min_resolution: "720p",
			allowed_codecs: ["hevc"],
			formats: [
				{ name: "x265", score: 100 },
				{ name: "x264", score: -1000 },
			],
			min_score: 0,
			upgrade_until_score: 100,
		},
	] as const satisfies readonly ProfilePreset[];
</script>

<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Plus, Trash2, WandSparkles } from "@lucide/svelte";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import Checkbox from "../../forms/Checkbox.svelte";
	import ScoreInput from "../../forms/ScoreInput.svelte";
	import { cn } from "../../../lib/cn";
	import { api } from "../../../lib/api";
	import { readOnlyLock } from "../../../lib/config.svelte";
	import FieldLock from "../../forms/FieldLock.svelte";
	import { VIDEO_CODECS } from "../../../lib/media-info";
	import type { CustomFormat } from "../../../lib/types";
	import type { AppForm } from "../../../lib/form";
	import { m as i18n } from "../../../lib/paraglide/messages.js";
	import { INPUT_CLASS } from "../../../lib/form";

	type Props = {
		form: AppForm<QualityProfileValues>;
		// Presets prefill a profile that does not exist yet; on an edit they would
		// silently discard scores the operator tuned.
		isCreate?: boolean;
		// True when the profile being edited already stores a transcode policy.
		// The API has no way to remove one — omitting `transcode` on a PUT leaves
		// the stored policy alone — so the enable toggle has to stay on rather
		// than offer an off that would not persist.
		policyPersisted?: boolean;
	};
	let { form, isCreate = false, policyPersisted = false }: Props = $props();

	const RESOLUTIONS: Resolution[] = ["720p", "1080p", "2160p"];

	// The transcode enums are the policy's own, not the profile's allow-list:
	// `if.video_codecs` covers seven source codecs (VIDEO_CODECS has the five
	// worth grabbing), and `to.video_codec` only three the encoder can produce.
	const SOURCE_CODECS = [
		{ value: "h264", label: "H.264" },
		{ value: "hevc", label: "HEVC" },
		{ value: "av1", label: "AV1" },
		{ value: "vp9", label: "VP9" },
		{ value: "mpeg4", label: "MPEG-4" },
		{ value: "mpeg2video", label: "MPEG-2" },
		{ value: "vc1", label: "VC-1" },
	];
	const SOURCE_CONTAINERS = ["mkv", "mp4", "avi", "mov", "ts", "m2ts", "webm", "wmv"];
	const TARGET_CONTAINERS: TranscodeTargetContainer[] = ["mkv", "mp4"];
	const TARGET_CODECS: { value: TranscodeTargetCodec; label: string }[] = [
		{ value: "hevc", label: "HEVC" },
		{ value: "h264", label: "H.264" },
		{ value: "av1", label: "AV1" },
	];
	const PRESETS: TranscodePreset[] = [
		"ultrafast",
		"superfast",
		"veryfast",
		"faster",
		"fast",
		"medium",
		"slow",
		"slower",
		"veryslow",
	];
	const AUDIO_CODECS: TranscodeAudioCodec[] = ["aac", "opus", "ac3", "flac"];
	// config.DefaultAudioPassthrough — what an empty list resolves to server-side.
	const PASSTHROUGH_CODECS = [
		"truehd",
		"eac3",
		"ac3",
		"dts",
		"aac",
		"opus",
		"flac",
	];

	// Labels are human, stored values are ffprobe's — mapped once, here, so the
	// codec check on the backend needs no table of its own.
	const lock = readOnlyLock();
	let locked = $derived(lock());


	const formats = createQuery<CustomFormat[]>(() => ({
		queryKey: ["custom-formats"],
		queryFn: () => api<CustomFormat[]>("/custom-formats"),
		staleTime: 60_000,
	}));

	// A profile may score a format that has since been deleted from the config;
	// keeping its name in the options is what lets the row still render its own
	// value instead of reading as an empty select.
	function optionsFor(picked: string[]) {
		const byName = new Map((formats.data ?? []).map((f) => [f.name, f]));
		const known = [...byName.keys()];
		const extra = picked.filter((n) => n && !byName.has(n));
		return [...known, ...extra].map((n) => ({
			value: n,
			label: n,
			hint: byName.get(n)?.description,
		}));
	}

	function toggleCodec(current: string[], value: string): string[] {
		return current.includes(value)
			? current.filter((c) => c !== value)
			: [...current, value];
	}

	function applyPreset(p: ProfilePreset) {
		form.setFieldValue("name", p.label);
		form.setFieldValue("preferred_resolution", p.preferred_resolution);
		form.setFieldValue("min_resolution", p.min_resolution);
		form.setFieldValue("allowed_codecs", [...p.allowed_codecs]);
		form.setFieldValue(
			"formats",
			p.formats.map((f) => ({ ...f })),
		);
		form.setFieldValue("min_score", p.min_score);
		form.setFieldValue("upgrade_until_score", p.upgrade_until_score);
	}
</script>

<div class="space-y-4">
	{#if isCreate && !locked}
		<div class="rounded-lg border border-border bg-bg-card p-3">
			<span class="flex items-center gap-1.5 text-sm font-medium text-fg">
				<WandSparkles size={14} class="text-fg-muted" aria-hidden="true" />
				{i18n.quality_presets()}
			</span>
			<p class="mt-0.5 text-xs leading-relaxed text-fg-muted">
				{i18n.quality_presets_help()}
			</p>
			<div class="mt-2 flex flex-wrap gap-2">
				{#each PROFILE_PRESETS as p (p.label)}
					<button
						type="button"
						onclick={() => applyPreset(p)}
						class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-full border border-border bg-bg-elevated px-3.5 text-sm font-medium text-fg transition hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
					>
						{p.label}
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<form.Field name="name">
		{#snippet children(field)}
			<TextField {field} label={i18n.common_name()} placeholder="1080p preferred" />
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="preferred_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label={i18n.quality_preferred_resolution()}
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.quality_preferred_help()}
					</p>
				</div>
			{/snippet}
		</form.Field>

		<form.Field name="min_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label={i18n.quality_minimum_resolution()}
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.quality_minimum_help()}
					</p>
				</div>
			{/snippet}
		</form.Field>
	</div>

	<form.Field name="allowed_codecs">
		{#snippet children(field)}
			{@const picked = field.state.value ?? []}
			<div>
				<span
					class="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-fg"
				>
					{i18n.quality_allowed_codecs()}
					<FieldLock locked={locked} />
				</span>
				<div class="flex flex-wrap gap-2">
					{#each VIDEO_CODECS as codec (codec.value)}
						{@const on = picked.includes(codec.value)}
						<button
							type="button"
							disabled={locked}
							aria-pressed={on}
							onclick={() =>
								field.handleChange(toggleCodec(picked, codec.value))}
							class={cn(
								"inline-flex h-11 items-center rounded-full border px-4 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
								on
									? "border-accent-line bg-accent-soft text-accent-text"
									: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong",
							)}
						>
							{codec.label}
						</button>
					{/each}
				</div>
				<!-- The empty case is the default and has no control of its own, so the
				     help line has to say what it means. An "Any codec" chip would be a
				     chip that is not a codec sitting in a row of codecs, and there is no
				     value for it to store. -->
				<p class="mt-1.5 text-xs text-fg-muted">
					{picked.length === 0
						? i18n.quality_codecs_any()
						: i18n.quality_codecs_n({
								count: picked.length,
								total: VIDEO_CODECS.length,
							})}
				</p>
			</div>
		{/snippet}
	</form.Field>

	<form.Field name="formats">
		{#snippet children(field)}
			{@const rows = field.state.value ?? []}
			{@const options = optionsFor(rows.map((r) => r.name))}
			<div>
				<div class="flex flex-wrap items-end justify-between gap-2">
					<div>
						<span class="flex items-center gap-1.5 text-sm font-medium text-fg">
							{i18n.quality_formats()}
							<FieldLock locked={locked} />
						</span>
						<p class="mt-0.5 max-w-xl text-xs leading-relaxed text-fg-muted">
							{i18n.quality_formats_help()}
						</p>
					</div>
					<button
						type="button"
						disabled={locked || options.length === 0}
						onclick={() =>
							field.handleChange([
								...rows,
								{ name: options[0]?.value ?? "", score: 0 },
							])}
						class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-sm font-medium text-fg transition hover:border-border-strong disabled:cursor-not-allowed disabled:opacity-60"
					>
						<Plus size={15} aria-hidden="true" />
						{i18n.quality_add_format()}
					</button>
				</div>

				<div class="mt-2.5 space-y-2">
					{#if rows.length === 0}
						<p
							class="rounded-lg border border-dashed border-border bg-bg-deep/40 px-3 py-4 text-center text-xs text-fg-muted"
						>
							{formats.isPending
								? i18n.common_loading()
								: i18n.quality_formats_none()}
						</p>
					{:else}
						{#each rows as row, i (i)}
							<div class="flex items-center gap-2">
								<div class="min-w-0 flex-1">
									<Select
										ariaLabel={i18n.quality_format_name()}
										value={row.name}
										{options}
										onChange={(n) =>
											field.handleChange(
												rows.map((r, k) => (k === i ? { ...r, name: n } : r)),
											)}
									/>
								</div>
								<div class="w-28 shrink-0">
									<ScoreInput
										ariaLabel={i18n.quality_score()}
										value={row.score ?? 0}
										readonly={locked}
										onChange={(n) =>
											field.handleChange(
												rows.map((r, k) => (k === i ? { ...r, score: n } : r)),
											)}
										class="{INPUT_CLASS} text-right font-mono tabular"
									/>
								</div>
								<button
									type="button"
									disabled={locked}
									onclick={() =>
										field.handleChange(rows.filter((_, k) => k !== i))}
									class="grid h-11 w-11 lg:h-9 lg:w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-40"
									aria-label={i18n.quality_remove_format()}
								>
									<Trash2 size={16} aria-hidden="true" />
								</button>
							</div>
						{/each}
					{/if}
				</div>
			</div>
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="min_score">
			{#snippet children(field)}
				<label class="block">
					<span
						class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
					>
						{i18n.quality_min_score()}
						<FieldLock locked={locked} />
					</span>
					<ScoreInput
						ariaLabel={i18n.quality_min_score()}
						value={field.state.value ?? 0}
						readonly={locked}
						onChange={(n) => field.handleChange(n)}
						class="{INPUT_CLASS} font-mono tabular"
					/>
					<p class="mt-1 text-xs text-fg-muted">{i18n.quality_min_score_help()}</p>
				</label>
			{/snippet}
		</form.Field>

		<form.Field name="upgrade_until_score">
			{#snippet children(field)}
				<label class="block">
					<span
						class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
					>
						{i18n.quality_upgrade_until_score()}
						<FieldLock locked={locked} />
					</span>
					<ScoreInput
						ariaLabel={i18n.quality_upgrade_until_score()}
						value={field.state.value ?? 0}
						readonly={locked}
						onChange={(n) => field.handleChange(n)}
						class="{INPUT_CLASS} font-mono tabular"
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.quality_upgrade_until_help()}
					</p>
				</label>
			{/snippet}
		</form.Field>
	</div>

	<form.Field name="upgrade_allowed">
		{#snippet children(field)}
			<Checkbox
				name={field.name}
				checked={field.state.value}
				onChange={(v) => field.handleChange(v)}
				label={i18n.quality_allow_upgrades()}
				description={i18n.quality_upgrades_help()}
			/>
		{/snippet}
	</form.Field>

	<div class="rounded-lg border border-border bg-bg-card p-3">
		<form.Field name="transcode_enabled">
			{#snippet children(field)}
				<Checkbox
					name={field.name}
					checked={field.state.value}
					disabled={locked || policyPersisted}
					onChange={(v) => field.handleChange(v)}
					label={i18n.quality_transcode_enable()}
					description={i18n.quality_transcode_help()}
				/>
			{/snippet}
		</form.Field>

		{#if policyPersisted}
			<p class="mt-2 text-xs leading-relaxed text-fg-muted">
				{i18n.quality_transcode_locked()}
			</p>
		{/if}

		<form.Subscribe selector={(s) => s.values.transcode_enabled}>
			{#snippet children(enabled)}
				{#if enabled}
					<div class="mt-4 space-y-4 border-t border-border pt-4">
						<div>
							<h4
								class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
							>
								{i18n.quality_transcode_if()}
							</h4>
							<p class="mt-1 text-xs leading-relaxed text-fg-muted">
								{i18n.quality_transcode_hdr_note()}
							</p>
						</div>

						<form.Field name="transcode.if.video_codecs">
							{#snippet children(field)}
								{@const picked = field.state.value ?? []}
								{@render chips(
									i18n.quality_transcode_codecs(),
									SOURCE_CODECS,
									picked,
									(v) => field.handleChange(toggleCodec(picked, v)),
									picked.length === 0
										? i18n.quality_transcode_codecs_any()
										: undefined,
								)}
							{/snippet}
						</form.Field>

						<form.Field name="transcode.if.containers">
							{#snippet children(field)}
								{@const picked = field.state.value ?? []}
								{@render chips(
									i18n.quality_transcode_containers(),
									SOURCE_CONTAINERS.map((c) => ({ value: c, label: c })),
									picked,
									(v) => field.handleChange(toggleCodec(picked, v)),
									picked.length === 0
										? i18n.quality_transcode_containers_any()
										: undefined,
								)}
							{/snippet}
						</form.Field>

						<form.Field name="transcode.if.max_video_bitrate">
							{#snippet children(field)}
								<TextField
									{field}
									label={i18n.quality_transcode_bitrate()}
									placeholder="8M"
									help={i18n.quality_transcode_bitrate_help()}
								/>
							{/snippet}
						</form.Field>

						<h4
							class="border-t border-border pt-4 font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
						>
							{i18n.quality_transcode_to()}
						</h4>

						<div class="grid gap-3 sm:grid-cols-2">
							<form.Field name="transcode.to.container">
								{#snippet children(field)}
									<Select
										label={i18n.quality_transcode_container()}
										value={field.state.value}
										options={TARGET_CONTAINERS.map((c) => ({
											value: c,
											label: c,
										}))}
										onChange={(v) => field.handleChange(v)}
										disabled={locked}
									/>
								{/snippet}
							</form.Field>

							<form.Field name="transcode.to.video_codec">
								{#snippet children(field)}
									<Select
										label={i18n.quality_transcode_video_codec()}
										value={field.state.value}
										options={TARGET_CODECS}
										onChange={(v) => field.handleChange(v)}
										disabled={locked}
									/>
								{/snippet}
							</form.Field>

							<form.Field name="transcode.to.preset">
								{#snippet children(field)}
									<Select
										label={i18n.quality_transcode_preset()}
										value={field.state.value}
										options={PRESETS.map((p) => ({ value: p, label: p }))}
										onChange={(v) => field.handleChange(v)}
										disabled={locked}
									/>
								{/snippet}
							</form.Field>

							<form.Field name="transcode.to.audio_codec">
								{#snippet children(field)}
									<Select
										label={i18n.quality_transcode_audio_codec()}
										value={field.state.value}
										options={AUDIO_CODECS.map((c) => ({ value: c, label: c }))}
										onChange={(v) => field.handleChange(v)}
										disabled={locked}
									/>
								{/snippet}
							</form.Field>
						</div>

						<form.Field name="transcode.to.crf">
							{#snippet children(field)}
								<label class="block">
									<span
										class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
									>
										{i18n.quality_transcode_crf()}
										<FieldLock locked={locked} />
									</span>
									<ScoreInput
										ariaLabel={i18n.quality_transcode_crf()}
										value={field.state.value ?? 0}
										readonly={locked}
										onChange={(n) => field.handleChange(n)}
										class="{INPUT_CLASS} font-mono tabular"
									/>
									<p class="mt-1 text-xs leading-relaxed text-fg-muted">
										{i18n.quality_transcode_crf_help()}
									</p>
								</label>
							{/snippet}
						</form.Field>

						<form.Field name="transcode.to.audio_passthrough">
							{#snippet children(field)}
								{@const picked = field.state.value ?? []}
								{@render chips(
									i18n.quality_transcode_passthrough(),
									PASSTHROUGH_CODECS.map((c) => ({ value: c, label: c })),
									picked,
									(v) => field.handleChange(toggleCodec(picked, v)),
									picked.length === 0
										? i18n.quality_transcode_passthrough_any()
										: undefined,
								)}
							{/snippet}
						</form.Field>
					</div>
				{/if}
			{/snippet}
		</form.Subscribe>
	</div>
</div>

{#snippet chips(
	label: string,
	options: { value: string; label: string }[],
	picked: string[],
	onToggle: (value: string) => void,
	emptyHint?: string,
)}
	<div>
		<span class="mb-1.5 flex items-center gap-1.5 text-sm font-medium text-fg">
			{label}
			<FieldLock locked={locked} />
		</span>
		<div class="flex flex-wrap gap-2">
			{#each options as opt (opt.value)}
				{@const on = picked.includes(opt.value)}
				<button
					type="button"
					disabled={locked}
					aria-pressed={on}
					onclick={() => onToggle(opt.value)}
					class={cn(
						"inline-flex h-11 lg:h-9 items-center rounded-full border px-4 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
						on
							? "border-accent-line bg-accent-soft text-accent-text"
							: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong",
					)}
				>
					{opt.label}
				</button>
			{/each}
		</div>
		{#if emptyHint}
			<p class="mt-1.5 text-xs text-fg-muted">{emptyHint}</p>
		{/if}
	</div>
{/snippet}
