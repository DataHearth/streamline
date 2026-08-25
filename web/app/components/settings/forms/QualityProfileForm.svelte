<script lang="ts" module>
	import type {
		QualityProfileFormatScore,
		Resolution,
	} from "../../../lib/types";

	export type QualityProfileValues = {
		name: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		upgrade_allowed: boolean;
		replace_whole_season: boolean;
		allowed_codecs: string[];
		formats: QualityProfileFormatScore[];
		min_score: number;
		upgrade_until_score: number;
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
	import { Plus, Trash2, Wand2 } from "@lucide/svelte";
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

	type Props = {
		form: AppForm<QualityProfileValues>;
		// Presets prefill a profile that does not exist yet; on an edit they would
		// silently discard scores the operator tuned.
		isCreate?: boolean;
	};
	let { form, isCreate = false }: Props = $props();

	const RESOLUTIONS: Resolution[] = ["720p", "1080p", "2160p"];

	// Labels are human, stored values are ffprobe's — mapped once, here, so the
	// codec check on the backend needs no table of its own.
	const lock = readOnlyLock();
	let locked = $derived(lock());

	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

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
				<Wand2 size={14} class="text-fg-muted" aria-hidden="true" />
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
						class="inline-flex h-9 items-center rounded-full border border-border bg-bg-elevated px-3.5 text-sm font-medium text-fg transition hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
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
						class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-sm font-medium text-fg transition hover:border-border-strong disabled:cursor-not-allowed disabled:opacity-60"
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
										class="{inputClass} text-right font-mono tabular"
									/>
								</div>
								<button
									type="button"
									disabled={locked}
									onclick={() =>
										field.handleChange(rows.filter((_, k) => k !== i))}
									class="grid h-9 w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-40"
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
						class="{inputClass} font-mono tabular"
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
						class="{inputClass} font-mono tabular"
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

	<form.Field name="replace_whole_season">
		{#snippet children(field)}
			<Checkbox
				name={field.name}
				checked={field.state.value}
				onChange={(v) => field.handleChange(v)}
				label={i18n.quality_replace_whole_season()}
				description={i18n.quality_replace_whole_season_help()}
			/>
		{/snippet}
	</form.Field>
</div>
