<script lang="ts" module>
	import type {
		CustomFormat,
		CustomFormatCondition,
		CustomFormatConditionType,
	} from "../../lib/types";
	import { m as messages } from "../../lib/paraglide/messages.js";

	export const CONDITION_TYPES: CustomFormatConditionType[] = [
		"release_title",
		"release_group",
		"resolution",
		"source",
		"codec",
		"size",
		"seeders",
	];

	export function conditionTypeLabel(t: CustomFormatConditionType): string {
		switch (t) {
			case "release_title":
				return messages.cf_type_release_title();
			case "release_group":
				return messages.cf_type_release_group();
			case "resolution":
				return messages.cf_type_resolution();
			case "source":
				return messages.cf_type_source();
			case "codec":
				return messages.cf_type_codec();
			case "size":
				return messages.cf_type_size();
			case "seeders":
				return messages.cf_type_seeders();
		}
	}

	// A row keeps every field the API knows about, whatever its type reads, so
	// switching a row's type and switching back restores what was typed. The
	// stripping happens once, in toConditions, on the way to the API.
	export type ConditionDraft = {
		type: CustomFormatConditionType;
		pattern: string;
		value: string;
		min_gb: number;
		max_gb: number;
		min: number;
		required: boolean;
		negate: boolean;
		// Editor-only, never sent: a release_group row whose pattern the chip
		// compiler could not have written is edited as raw regex instead.
		groupRaw: boolean;
	};

	// The chip compiler's alphabet. A group name is a literal, so every RE2
	// metacharacter in one is escaped on the way out and has to come back
	// escaped on the way in — an unescaped one means the pattern is somebody's
	// own regex, not a list we rendered.
	const REGEX_META = /[.*+?^${}()|[\]\\]/;
	const GROUP_PATTERN = /^\(\?i\)\^\((.*)\)\$$/;

	function escapeGroupName(name: string): string {
		return name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	}

	// Compiles chips to the anchored, case-insensitive alternation the
	// release_group condition matches against. No chips means no pattern, which
	// the row's own "Pattern required" check then reports.
	export function groupsToPattern(names: string[]): string {
		const clean = names.map((n) => n.trim()).filter((n) => n !== "");
		if (clean.length === 0) return "";
		return `(?i)^(${clean.map(escapeGroupName).join("|")})$`;
	}

	// The inverse, and deliberately strict: anything it cannot prove it could
	// have emitted returns null so the row falls back to the regex input rather
	// than rendering a lossy chip list over a pattern it would then overwrite.
	export function parseGroupPattern(pattern: string): string[] | null {
		const m = GROUP_PATTERN.exec(pattern.trim());
		if (!m) return null;
		const body = m[1];
		// GROUP_PATTERN's one capture group is unconditional (not inside `?:` or
		// an alternation), so a match always populates it.
		if (body === undefined) return null;
		const names: string[] = [];
		let cur = "";
		for (let i = 0; i < body.length; i++) {
			// i < body.length, so the index is always in range.
			const ch = body[i]!;
			if (ch === "\\") {
				const next = body[i + 1];
				// \d, \b, \w and friends are classes, not escaped literals.
				if (next === undefined || /[a-zA-Z0-9]/.test(next)) return null;
				cur += next;
				i++;
				continue;
			}
			if (ch === "|") {
				names.push(cur);
				cur = "";
				continue;
			}
			if (REGEX_META.test(ch)) return null;
			cur += ch;
		}
		names.push(cur);
		return names.every((n) => n !== "") ? names : null;
	}

	export type CustomFormatDraft = {
		name: string;
		description: string;
		conditions: ConditionDraft[];
	};

	export function newCondition(): ConditionDraft {
		return {
			type: "release_title",
			pattern: "",
			value: "",
			min_gb: 0,
			max_gb: 0,
			min: 0,
			required: true,
			negate: false,
			groupRaw: false,
		};
	}

	export function draftFrom(f: CustomFormat | null): CustomFormatDraft {
		if (!f) return { name: "", description: "", conditions: [newCondition()] };
		return {
			name: f.name,
			description: f.description ?? "",
			conditions: f.conditions.map((c) => {
				const pattern = c.pattern ?? "";
				return {
					...newCondition(),
					type: c.type,
					pattern,
					value: c.value ?? "",
					min_gb: c.min_gb ?? 0,
					max_gb: c.max_gb ?? 0,
					min: c.min ?? 0,
					required: c.required ?? false,
					negate: c.negate ?? false,
					groupRaw:
						c.type === "release_group" &&
						pattern !== "" &&
						parseGroupPattern(pattern) === null,
				};
			}),
		};
	}

	// toConditions drops the fields a row's type ignores. Sending them would
	// store a pattern on a size condition — harmless to the matcher, but it
	// comes back on the next GET and reads as if it were in play.
	export function toConditions(
		rows: ConditionDraft[],
	): CustomFormatCondition[] {
		return rows.map((c) => {
			const out: CustomFormatCondition = { type: c.type };
			if (c.required) out.required = true;
			if (c.negate) out.negate = true;
			switch (c.type) {
				case "release_title":
				case "release_group":
					out.pattern = c.pattern.trim();
					break;
				case "resolution":
				case "source":
				case "codec":
					out.value = c.value.trim();
					break;
				case "size":
					if (c.min_gb > 0) out.min_gb = c.min_gb;
					if (c.max_gb > 0) out.max_gb = c.max_gb;
					break;
				case "seeders":
					out.min = c.min;
					break;
			}
			return out;
		});
	}
</script>

<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { Check, Plus, Trash2, X, Ban, Asterisk } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import { config } from "../../lib/config.svelte";
	import { customFormatCondition } from "../../lib/schemas";
	import type { CustomFormatTestResult, Resolution } from "../../lib/types";
	import Select from "../forms/Select.svelte";
	import FieldLock from "../forms/FieldLock.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		// The page owns the draft; the editor mutates it in place, which is what
		// $bindable licenses.
		draft: CustomFormatDraft;
		// The name is the resource key and the API has no rename, so it is fixed
		// once the format exists.
		isEdit?: boolean;
	};

	let { draft = $bindable(), isEdit = false }: Props = $props();

	// Plain inputs rather than TextField: that primitive needs a TanStack field
	// API, and a format is a variable-length list of heterogeneous rows — a form
	// library's field paths would have to be rebuilt on every type switch.
	const inputClass =
		"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70";

	let locked = $derived(config.readOnly);

	// EXAMPLE_EPISODES is the season length the size hint reasons about. The
	// bounds are typed per episode and multiplied at match time, so the number
	// that actually gates a pack is one the operator never sees — this spells
	// the multiplication out against a plausible season rather than leaving it
	// as arithmetic they have to do in their head.
	const EXAMPLE_EPISODES = 12;

	function roundGB(n: number): string {
		return String(Math.round(n * 10) / 10);
	}

	// sizeScaleHint renders what a pack of EXAMPLE_EPISODES would be measured
	// against, or null while neither bound is set and there is nothing to scale.
	function sizeScaleHint(c: ConditionDraft): string | null {
		const lo = c.min_gb > 0 ? c.min_gb * EXAMPLE_EPISODES : 0;
		const hi = c.max_gb > 0 ? c.max_gb * EXAMPLE_EPISODES : 0;
		if (lo <= 0 && hi <= 0) return null;
		let range: string;
		if (lo > 0 && hi > 0) range = `${roundGB(lo)}–${roundGB(hi)} GB`;
		else if (hi > 0) range = i18n.cf_size_at_most({ gb: roundGB(hi) });
		else range = i18n.cf_size_at_least({ gb: roundGB(lo) });
		return i18n.cf_size_scale({ episodes: EXAMPLE_EPISODES, range });
	}

	const TYPES = CONDITION_TYPES.map((t) => ({
		value: t,
		label: conditionTypeLabel(t),
	}));

	const RESOLUTIONS: Resolution[] = ["720p", "1080p", "2160p"];

	// The parser normalises what it lifts out of a release name to these spellings
	// (internal/library/parser.go); the comparison is case-insensitive, and the
	// field stays free text so an indexer's own vocabulary still works.
	const SOURCES = ["BluRay", "Remux", "WEB-DL", "WEBRip", "WEB", "HDTV", "DVDRip"];
	const CODECS = ["x264", "HEVC", "AV1", "MPEG2", "VC-1"];

	// A resolution row has a closed vocabulary, so switching into it lands on a
	// value rather than on an empty select that reads as chosen but validates as
	// missing.
	function setType(c: ConditionDraft, t: CustomFormatConditionType) {
		c.type = t;
		if (t === "resolution" && !RESOLUTIONS.includes(c.value as Resolution)) {
			c.value = "1080p";
		}
		// Switching a typed release_title regex over to release_group must not
		// land on an empty chip row that overwrites it on the first chip.
		if (t === "release_group") {
			c.groupRaw = c.pattern !== "" && parseGroupPattern(c.pattern) === null;
		}
	}

	// ── Release-group chips ───────────────────────────────────
	// Keyed by row index, like the {#each} itself: the pending text is DOM state
	// of the row at that position, and Svelte reuses the row's DOM the same way.
	let pendingGroup = $state<Record<number, string>>({});

	function groupChips(c: ConditionDraft): string[] {
		return parseGroupPattern(c.pattern) ?? [];
	}

	function commitGroups(c: ConditionDraft, i: number) {
		const names = groupChips(c);
		for (const part of (pendingGroup[i] ?? "").split(",")) {
			const n = part.trim();
			if (n !== "" && !names.includes(n)) names.push(n);
		}
		pendingGroup[i] = "";
		c.pattern = groupsToPattern(names);
	}

	function removeGroup(c: ConditionDraft, at: number) {
		const names = groupChips(c);
		names.splice(at, 1);
		c.pattern = groupsToPattern(names);
	}

	function onGroupKey(e: KeyboardEvent, c: ConditionDraft, i: number) {
		const pending = pendingGroup[i] ?? "";
		if (e.key === "Enter") {
			// The editor sits inside a <form>; Enter here means "add this chip".
			e.preventDefault();
			commitGroups(c, i);
		} else if (e.key === "Backspace" && pending === "") {
			const names = groupChips(c);
			if (names.length > 0) removeGroup(c, names.length - 1);
		}
	}

	// required and negate are orthogonal: negate flips this condition's own
	// verdict, required decides whether that verdict joins the all-must-pass
	// group or the at-least-one group. The four combinations are four rules,
	// which the two toggle labels alone don't say.
	function ruleText(c: ConditionDraft): string {
		if (c.required) {
			return c.negate ? i18n.cf_rule_must_not() : i18n.cf_rule_must();
		}
		return c.negate ? i18n.cf_rule_any_not() : i18n.cf_rule_any();
	}

	function rowError(c: ConditionDraft): string | null {
		const r = v.safeParse(customFormatCondition, $state.snapshot(c));
		if (r.success) return null;
		return r.issues[0]?.message ?? i18n.cf_condition_invalid();
	}

	function numberValue(n: number): string {
		return n > 0 ? String(n) : "";
	}

	function readNumber(raw: string): number {
		const n = Number(raw);
		return raw === "" || !Number.isFinite(n) || n < 0 ? 0 : n;
	}

	let rowErrors = $derived(draft.conditions.map(rowError));

	// ── Tester ────────────────────────────────────────────────
	let sampleTitle = $state("");
	let sampleSizeGB = $state("");
	let sampleSeeders = $state("");
	// Size bounds are multiplied by this, so a pack can be tested against the
	// same per-episode threshold the real search would use.
	let sampleEpisodes = $state("");

	type TestPayload = {
		conditions: CustomFormatCondition[];
		sample: {
			title: string;
			size?: number;
			seeders?: number;
			episodes?: number;
		};
	};

	let livePayload = $derived.by<TestPayload | null>(() => {
		const title = sampleTitle.trim();
		if (!title || draft.conditions.length === 0) return null;
		if (rowErrors.some((e) => e !== null)) return null;
		const sample: TestPayload["sample"] = { title };
		const gb = Number(sampleSizeGB);
		if (sampleSizeGB !== "" && Number.isFinite(gb) && gb > 0) {
			sample.size = Math.round(gb * 1024 ** 3);
		}
		const seeders = Number(sampleSeeders);
		if (sampleSeeders !== "" && Number.isFinite(seeders) && seeders >= 0) {
			sample.seeders = Math.round(seeders);
		}
		const episodes = Number(sampleEpisodes);
		if (sampleEpisodes !== "" && Number.isFinite(episodes) && episodes > 0) {
			sample.episodes = Math.round(episodes);
		}
		return {
			conditions: toConditions($state.snapshot(draft).conditions),
			sample,
		};
	});

	// Debounced so a typed release title is one request, not one per keystroke.
	let payload = $state<TestPayload | null>(null);
	$effect(() => {
		const next = livePayload;
		const t = setTimeout(() => (payload = next), 300);
		return () => clearTimeout(t);
	});

	let payloadKey = $derived(payload ? JSON.stringify(payload) : "");

	const test = createQuery<CustomFormatTestResult>(() => ({
		queryKey: ["custom-formats", "test", payloadKey],
		queryFn: () =>
			api<CustomFormatTestResult>("/custom-formats/test", {
				method: "POST",
				body: payload,
			}),
		enabled: payload !== null,
		retry: false,
		staleTime: 5 * 60_000,
	}));

	// Results are index-aligned with the conditions that were sent, so they only
	// describe the rows while the draft still matches what the request carried.
	let results = $derived(
		payloadKey && payloadKey === JSON.stringify(livePayload)
			? (test.data?.conditions ?? null)
			: null,
	);
</script>

<div class="space-y-5">
	<label class="block">
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
			{i18n.common_name()}
			<FieldLock locked={locked} />
		</span>
		<input
			type="text"
			spellcheck="false"
			autocapitalize="off"
			autocomplete="off"
			readonly={locked || isEdit}
			value={draft.name}
			placeholder="dolby-vision"
			oninput={(e) => (draft.name = (e.currentTarget as HTMLInputElement).value)}
			class={inputClass}
		/>
		<p class="mt-1 text-xs text-fg-muted">
			{isEdit ? i18n.cf_name_locked() : i18n.cf_name_help()}
		</p>
	</label>

	<label class="block">
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
			{i18n.cf_description()}
			<FieldLock locked={locked} />
		</span>
		<input
			type="text"
			readonly={locked}
			value={draft.description}
			placeholder={i18n.cf_description_placeholder()}
			oninput={(e) =>
				(draft.description = (e.currentTarget as HTMLInputElement).value)}
			class={inputClass}
		/>
		<p class="mt-1 text-xs text-fg-muted">{i18n.cf_description_help()}</p>
	</label>

	<div>
		<div class="flex flex-wrap items-end justify-between gap-2">
			<div>
				<span class="flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.cf_conditions()}
					<FieldLock locked={locked} />
				</span>
				<p class="mt-0.5 max-w-xl text-xs leading-relaxed text-fg-muted">
					{i18n.cf_conditions_help()}
				</p>
			</div>
			<button
				type="button"
				disabled={locked}
				onclick={() => draft.conditions.push(newCondition())}
				class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 text-sm font-medium text-fg transition hover:border-border-strong disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Plus size={15} aria-hidden="true" />
				{i18n.cf_add_condition()}
			</button>
		</div>

		<div class="mt-3 space-y-2">
			{#each draft.conditions as c, i (i)}
				{@const err = rowErrors[i]}
				{@const passed = results?.[i]?.passed}
				<div
					class={cn(
						"rounded-lg border bg-bg-deep/40 p-3",
						err ? "border-status-failed/50" : "border-border",
					)}
				>
					<div class="flex flex-wrap items-end gap-2">
						<div class="w-40 shrink-0">
							<Select
								ariaLabel={i18n.cf_type()}
								value={c.type}
								options={TYPES}
								onChange={(t) => setType(c, t)}
							/>
						</div>

						{#if c.type === "release_group" && !c.groupRaw}
							{@const chips = groupChips(c)}
							<div
								class={cn(
									"flex min-w-48 flex-1 flex-wrap items-center gap-1.5 rounded-md border border-border bg-bg px-2 py-1.5 focus-within:ring-2 focus-within:ring-accent",
									locked && "cursor-not-allowed opacity-70",
								)}
							>
								{#each chips as name, k (name)}
									<span
										class="inline-flex items-center gap-1 rounded-full border border-accent-line bg-accent-soft py-0.5 pl-2.5 pr-1 text-xs font-medium text-accent-text"
									>
										<span class="font-mono leading-none">{name}</span>
										<button
											type="button"
											disabled={locked}
											onclick={() => removeGroup(c, k)}
											class="grid h-4 w-4 place-items-center rounded-full text-accent-text/70 transition hover:bg-accent-line hover:text-accent-text disabled:cursor-not-allowed disabled:opacity-40"
											aria-label={i18n.cf_group_remove()}
										>
											<X size={11} aria-hidden="true" />
										</button>
									</span>
								{/each}
								<input
									type="text"
									spellcheck="false"
									autocapitalize="off"
									autocomplete="off"
									readonly={locked}
									value={pendingGroup[i] ?? ""}
									placeholder={chips.length === 0
										? i18n.cf_group_placeholder()
										: ""}
									aria-label={i18n.cf_groups()}
									oninput={(e) => {
										const raw = (e.currentTarget as HTMLInputElement).value;
										pendingGroup[i] = raw;
										if (raw.includes(",")) commitGroups(c, i);
									}}
									onkeydown={(e) => onGroupKey(e, c, i)}
									onblur={() => commitGroups(c, i)}
									class="min-w-32 flex-1 bg-transparent py-0.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none read-only:cursor-not-allowed"
								/>
							</div>
						{:else if c.type === "release_title" || c.type === "release_group"}
							<input
								type="text"
								spellcheck="false"
								autocapitalize="off"
								autocomplete="off"
								readonly={locked}
								value={c.pattern}
								placeholder={"(?i)\\bremux\\b"}
								aria-label={i18n.cf_pattern()}
								oninput={(e) =>
									(c.pattern = (e.currentTarget as HTMLInputElement).value)}
								class="{inputClass} min-w-48 flex-1 font-mono"
							/>
						{:else if c.type === "resolution"}
							<div class="w-36 shrink-0">
								<Select
									ariaLabel={i18n.cf_value()}
									value={(c.value || "1080p") as Resolution}
									options={RESOLUTIONS.map((r) => ({
										value: r,
										label: r,
									}))}
									onChange={(r) => (c.value = r)}
								/>
							</div>
						{:else if c.type === "source" || c.type === "codec"}
							<input
								type="text"
								list={c.type === "source" ? "cf-sources" : "cf-codecs"}
								spellcheck="false"
								autocapitalize="off"
								autocomplete="off"
								readonly={locked}
								value={c.value}
								placeholder={c.type === "source" ? "BluRay" : "HEVC"}
								aria-label={i18n.cf_value()}
								oninput={(e) =>
									(c.value = (e.currentTarget as HTMLInputElement).value)}
								class="{inputClass} min-w-40 flex-1 font-mono"
							/>
						{:else if c.type === "size"}
							<label class="w-28 shrink-0">
								<span class="mb-1 block text-[11px] font-medium text-fg-muted">
									{i18n.cf_min_gb()}
								</span>
								<input
									type="number"
									min="0"
									step="0.1"
									inputmode="decimal"
									readonly={locked}
									value={numberValue(c.min_gb)}
									oninput={(e) =>
										(c.min_gb = readNumber(
											(e.currentTarget as HTMLInputElement).value,
										))}
									class="{inputClass} font-mono tabular"
								/>
							</label>
							<label class="w-28 shrink-0">
								<span class="mb-1 block text-[11px] font-medium text-fg-muted">
									{i18n.cf_max_gb()}
								</span>
								<input
									type="number"
									min="0"
									step="0.1"
									inputmode="decimal"
									readonly={locked}
									value={numberValue(c.max_gb)}
									oninput={(e) =>
										(c.max_gb = readNumber(
											(e.currentTarget as HTMLInputElement).value,
										))}
									class="{inputClass} font-mono tabular"
								/>
							</label>
						{:else if c.type === "seeders"}
							<label class="w-32 shrink-0">
								<span class="mb-1 block text-[11px] font-medium text-fg-muted">
									{i18n.cf_min_seeders()}
								</span>
								<input
									type="number"
									min="1"
									inputmode="numeric"
									readonly={locked}
									value={numberValue(c.min)}
									oninput={(e) =>
										(c.min = readNumber(
											(e.currentTarget as HTMLInputElement).value,
										))}
									class="{inputClass} font-mono tabular"
								/>
							</label>
						{/if}

						<div class="ml-auto flex shrink-0 items-center gap-1">
							{#if passed !== undefined}
								<span
									class={cn(
										"inline-flex h-9 items-center gap-1 rounded-md px-2 text-[11px] font-semibold uppercase tracking-wide",
										passed
											? "bg-status-available/10 text-status-available"
											: "bg-surface text-fg-muted",
									)}
								>
									{#if passed}
										<Check size={13} aria-hidden="true" />
										{i18n.cf_pass()}
									{:else}
										<X size={13} aria-hidden="true" />
										{i18n.cf_fail()}
									{/if}
								</span>
							{/if}
							<button
								type="button"
								disabled={locked || draft.conditions.length === 1}
								onclick={() => draft.conditions.splice(i, 1)}
								class="grid h-9 w-9 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed disabled:cursor-not-allowed disabled:opacity-40"
								aria-label={i18n.cf_remove_condition()}
							>
								<Trash2 size={16} aria-hidden="true" />
							</button>
						</div>
					</div>

					<div class="mt-2 flex flex-wrap items-center gap-2">
						{@render toggle(
							c.required,
							(on) => (c.required = on),
							i18n.cf_required(),
							Asterisk,
						)}
						{@render toggle(
							c.negate,
							(on) => (c.negate = on),
							i18n.cf_negate(),
							Ban,
						)}
						<span class="text-[11px] font-medium text-fg-muted">
							{ruleText(c)}
						</span>
						{#if c.type === "size"}
							<span class="text-[11px] text-fg-subtle">{i18n.cf_size_help()}</span
							>
							{#if sizeScaleHint(c)}
								<span class="text-[11px] text-accent-text">
									{sizeScaleHint(c)}
								</span>
							{/if}
						{:else if c.type === "release_group"}
							<span class="text-[11px] text-fg-subtle">
								{c.groupRaw ? i18n.cf_pattern_help() : i18n.cf_group_help()}
							</span>
							{#if !c.groupRaw || c.pattern === "" || parseGroupPattern(c.pattern) !== null}
								<button
									type="button"
									onclick={() => (c.groupRaw = !c.groupRaw)}
									class="text-[11px] font-medium text-accent-text underline-offset-2 transition hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
								>
									{c.groupRaw
										? i18n.cf_group_use_chips()
										: i18n.cf_group_edit_regex()}
								</button>
							{/if}
						{:else if c.type === "release_title"}
							<span class="text-[11px] text-fg-subtle"
								>{i18n.cf_pattern_help()}</span
							>
						{/if}
					</div>

					{#if err}
						<p class="mt-2 text-xs text-status-failed">{err}</p>
					{/if}
				</div>
			{/each}
		</div>
	</div>

	<section class="rounded-lg border border-border bg-bg-card p-4">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h3 class="text-sm font-semibold text-fg">{i18n.cf_tester()}</h3>
			{#if test.isError}
				<span class="text-xs text-status-failed">{errorText(test.error)}</span>
			{:else if results}
				<span
					class={cn(
						"inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wide",
						test.data?.matched
							? "bg-status-available text-bg-deep"
							: "bg-surface text-fg-muted",
					)}
				>
					{test.data?.matched ? i18n.cf_matches() : i18n.cf_no_match()}
				</span>
			{/if}
		</div>
		<p class="mt-0.5 text-xs leading-relaxed text-fg-subtle">
			{i18n.cf_tester_help()}
		</p>

		<div class="mt-3 flex flex-wrap gap-2">
			<label class="min-w-56 flex-1">
				<span class="mb-1 block text-[11px] font-medium text-fg-muted">
					{i18n.cf_sample_title()}
				</span>
				<input
					type="text"
					spellcheck="false"
					autocapitalize="off"
					autocomplete="off"
					value={sampleTitle}
					oninput={(e) =>
						(sampleTitle = (e.currentTarget as HTMLInputElement).value)}
					placeholder="Movie.2024.2160p.BluRay.REMUX.HDR.x265-GRP"
					class="{inputClass} font-mono"
				/>
			</label>
			<label class="w-28">
				<span class="mb-1 block text-[11px] font-medium text-fg-muted">
					{i18n.cf_sample_size()}
				</span>
				<input
					type="number"
					min="0"
					step="0.1"
					inputmode="decimal"
					value={sampleSizeGB}
					oninput={(e) =>
						(sampleSizeGB = (e.currentTarget as HTMLInputElement).value)}
					class="{inputClass} font-mono tabular"
				/>
			</label>
			<label class="w-28">
				<span class="mb-1 block text-[11px] font-medium text-fg-muted">
					{i18n.cf_sample_seeders()}
				</span>
				<input
					type="number"
					min="0"
					inputmode="numeric"
					value={sampleSeeders}
					oninput={(e) =>
						(sampleSeeders = (e.currentTarget as HTMLInputElement).value)}
					class="{inputClass} font-mono tabular"
				/>
			</label>
			<label class="w-28">
				<span class="mb-1 block text-[11px] font-medium text-fg-muted">
					{i18n.cf_sample_episodes()}
				</span>
				<input
					type="number"
					min="1"
					inputmode="numeric"
					placeholder="1"
					value={sampleEpisodes}
					oninput={(e) =>
						(sampleEpisodes = (e.currentTarget as HTMLInputElement).value)}
					class="{inputClass} font-mono tabular"
				/>
			</label>
		</div>
		<p class="mt-1.5 text-[11px] text-fg-subtle">
			{i18n.cf_sample_episodes_help()}
		</p>

		{#if !sampleTitle.trim()}
			<p class="mt-2 text-xs text-fg-subtle">{i18n.cf_tester_idle()}</p>
		{:else if rowErrors.some((e) => e !== null)}
			<p class="mt-2 text-xs text-fg-subtle">{i18n.cf_tester_invalid()}</p>
		{/if}
	</section>
</div>

<datalist id="cf-sources">
	{#each SOURCES as s (s)}
		<option value={s}></option>
	{/each}
</datalist>
<datalist id="cf-codecs">
	{#each CODECS as c (c)}
		<option value={c}></option>
	{/each}
</datalist>

{#snippet toggle(
	on: boolean,
	set: (v: boolean) => void,
	label: string,
	Icon: typeof Check,
)}
	<label
		class={cn(
			"inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 text-[11px] font-medium transition",
			on
				? "border-accent-line bg-accent-soft text-accent-text"
				: "border-border bg-bg-base text-fg-muted hover:border-border-strong",
			locked && "cursor-not-allowed opacity-50",
		)}
	>
		<input
			type="checkbox"
			checked={on}
			disabled={locked}
			onchange={(e) => set((e.currentTarget as HTMLInputElement).checked)}
			class="sr-only"
		/>
		<Icon size={13} class="shrink-0" aria-hidden="true" />
		<span class="leading-none">{label}</span>
	</label>
{/snippet}

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
