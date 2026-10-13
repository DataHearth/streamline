<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Check, ListChecks, Tags } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { config, markConfigForm } from "@lib/config.svelte";
	import { toast } from "@lib/toast";
	import type {
		LibraryConfig,
		MusicTagsConfig,
		MusicTagsMode,
		MusicTagsSeeding,
	} from "@lib/types";
	import Select from "@components/forms/Select.svelte";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { INPUT_CLASS } from "@lib/form";
	import { releaseTypeLabel, type ReleaseType } from "@lib/music-books";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Saves per control, as on /settings/series, so nothing else establishes the
	// config-form context the field primitives read.
	markConfigForm();

	const TYPES: ReleaseType[] = ["album", "ep", "single", "live", "compilation", "other"];

	const qc = useQueryClient();

	const cfg = createQuery<LibraryConfig>(() => ({
		queryKey: ["config", "library"],
		queryFn: () => api<LibraryConfig>("/config/library"),
	}));

	const save = createMutation<LibraryConfig, Error, Partial<LibraryConfig>>(
		() => ({
			mutationFn: (body) =>
				api<LibraryConfig>("/config/library", { method: "PATCH", body }),
			onSuccess: (resp) => {
				qc.setQueryData(["config", "library"], resp);
				toast.ok(i18n.music_settings_saved());
			},
			onError: (err) => toast.err(errorText(err)),
		}),
	);

	// Kept in TYPES order, so the stored list reads the same however it was
	// ticked.
	function toggle(current: ReleaseType[], t: ReleaseType, on: boolean) {
		const next = TYPES.filter((x) => (x === t ? on : current.includes(x)));
		save.mutate({ music_monitored_types: next });
	}

	function saveTags(patch: Partial<MusicTagsConfig>) {
		save.mutate({ music_tags: patch as MusicTagsConfig });
	}

	// Key lists and the cover size are text, committed on blur; a draft keyed
	// by field keeps a refetch from overwriting what is being typed.
	let drafts = $state<Record<string, string>>({});

	function commitKeys(key: "strip" | "preserve", stored: string[]) {
		const raw = drafts[key];
		delete drafts[key];
		if (raw === undefined) return;
		const next = raw
			.split(/[\s,]+/)
			.map((s) => s.trim().toUpperCase())
			.filter(Boolean);
		if (next.join(" ") === stored.join(" ")) return;
		saveTags({ [key]: next });
	}

	function commitArtSize(stored: number) {
		const raw = drafts.art_max_px;
		delete drafts.art_max_px;
		if (raw === undefined) return;
		const n = Math.round(Number(raw));
		if (!Number.isFinite(n)) return;
		const clamped = Math.min(3000, Math.max(256, n));
		if (clamped === stored) return;
		saveTags({ art_max_px: clamped });
	}

	const retag = createMutation<{ queued: number }, Error, void>(() => ({
		mutationFn: () =>
			api<{ queued: number }>("/music/tags/retag", { method: "POST" }),
		onSuccess: ({ queued }) =>
			toast.ok(
				queued === 1
					? i18n.music_tags_retag_queued_one({ count: queued })
					: i18n.music_tags_retag_queued_other({ count: queued }),
			),
		onError: (err) => toast.err(errorText(err)),
	}));

	type ApplyResult = { monitored: number; unmonitored: number; types: ReleaseType[] };

	const applyExisting = createMutation<ApplyResult, Error, void>(() => ({
		mutationFn: () =>
			api<ApplyResult>("/music/monitored-types/apply", { method: "POST" }),
		onSuccess: ({ monitored: on, unmonitored: off }) => {
			qc.invalidateQueries({ queryKey: ["music"] });
			toast.ok(
				on + off === 0
					? i18n.music_types_none()
					: i18n.music_types_applied({ on, off }),
			);
		},
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<p class="text-sm text-fg-muted">
			{i18n.settings_music_intro()}
		</p>
	</header>

	{#if cfg.isPending}
		<div class="mt-6 space-y-4"><SkeletonList variant="panel" count={1} /></div>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(cfg.error) })}
		</p>
	{:else if cfg.data}
		{@const types = cfg.data.music_monitored_types ?? []}
		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<h2 class="text-sm font-medium text-fg">{i18n.music_monitored_types()}</h2>
			<p class="mt-1 text-xs text-fg-subtle">{i18n.music_monitored_types_help()}</p>
			<div class="mt-3 grid gap-2 sm:grid-cols-2">
				{#each TYPES as t (t)}
					<Checkbox
						checked={types.includes(t)}
						disabled={config.readOnly || save.isPending}
						onChange={(v) => toggle(types, t, v)}
						label={releaseTypeLabel(t)}
					/>
				{/each}
			</div>
			<div
				class="mt-3 border-t border-border pt-3 text-xs text-fg-subtle"
			>
				<p class="flex items-center gap-1.5">
					<Check size={12} aria-hidden="true" />
					{i18n.settings_music_applies()}
				</p>
				<div class="mt-2.5 flex flex-wrap items-center gap-2">
					<button
						type="button"
						disabled={applyExisting.isPending}
						onclick={() => applyExisting.mutate()}
						class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-bg px-2.5 text-xs font-medium text-fg transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-60"
					>
						<ListChecks size={13} aria-hidden="true" />
						{applyExisting.isPending
							? i18n.common_applying()
							: i18n.music_apply_existing()}
					</button>
					<span>{i18n.music_apply_existing_help()}</span>
				</div>
			</div>
		</section>

		{@const tags = cfg.data.music_tags}
		<section class="mt-4 space-y-4 rounded-lg border border-border bg-bg-card p-4">
			<div>
				<h2 class="text-sm font-medium text-fg">{i18n.music_tags_title()}</h2>
				<p class="mt-1 text-xs text-fg-subtle">{i18n.music_tags_help()}</p>
			</div>

			<div class="max-w-sm">
				<Select
					label={i18n.music_tags_mode()}
					value={tags.mode}
					options={[
						{ value: "off", label: i18n.music_tags_mode_off(), hint: i18n.music_tags_mode_off_hint() },
						{ value: "import", label: i18n.music_tags_mode_import(), hint: i18n.music_tags_mode_import_hint() },
						{ value: "managed", label: i18n.music_tags_mode_managed(), hint: i18n.music_tags_mode_managed_hint() },
					]}
					onChange={(v) => saveTags({ mode: v as MusicTagsMode })}
				/>
			</div>

			<div class="max-w-sm">
				<Select
					label={i18n.music_tags_seeding()}
					value={tags.seeding}
					disabled={tags.mode === "off"}
					options={[
						{ value: "defer", label: i18n.music_tags_seeding_defer(), hint: i18n.music_tags_seeding_defer_hint() },
						{ value: "copy", label: i18n.music_tags_seeding_copy(), hint: i18n.music_tags_seeding_copy_hint() },
					]}
					onChange={(v) => saveTags({ seeding: v as MusicTagsSeeding })}
				/>
			</div>

			<Checkbox
				checked={tags.embed_art}
				disabled={config.readOnly || save.isPending || tags.mode === "off"}
				onChange={(v) => saveTags({ embed_art: v })}
				label={i18n.music_tags_embed_art()}
				description={i18n.music_tags_embed_art_help()}
			/>

			<label class="block">
				<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.music_tags_art_size()}
					<FieldLock locked={config.readOnly} />
				</span>
				<span class="block w-28">
					<input
						type="number"
						inputmode="numeric"
						min="256"
						max="3000"
						readonly={config.readOnly}
						value={drafts.art_max_px ?? String(tags.art_max_px)}
						oninput={(e) => (drafts.art_max_px = (e.currentTarget as HTMLInputElement).value)}
						onblur={() => commitArtSize(tags.art_max_px)}
						onkeydown={(e) => {
							if (e.key === "Enter") (e.currentTarget as HTMLInputElement).blur();
						}}
						class="{INPUT_CLASS} font-mono"
					/>
				</span>
				<p class="mt-1 text-xs text-fg-subtle">{i18n.music_tags_art_size_help()}</p>
			</label>

			<Checkbox
				checked={tags.sidecar_art}
				disabled={config.readOnly || save.isPending || tags.mode === "off"}
				onChange={(v) => saveTags({ sidecar_art: v })}
				label={i18n.music_tags_sidecar_art()}
				description={i18n.music_tags_sidecar_art_help()}
			/>

			{#each [
				{ key: "strip" as const, label: i18n.music_tags_strip(), help: i18n.music_tags_strip_help(), stored: tags.strip },
				{ key: "preserve" as const, label: i18n.music_tags_preserve(), help: i18n.music_tags_preserve_help(), stored: tags.preserve },
			] as f (f.key)}
				<label class="block">
					<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg">
						{f.label}
						<FieldLock locked={config.readOnly} />
					</span>
					<textarea
						rows="2"
						spellcheck="false"
						readonly={config.readOnly}
						value={drafts[f.key] ?? f.stored.join(" ")}
						oninput={(e) => (drafts[f.key] = (e.currentTarget as HTMLTextAreaElement).value)}
						onblur={() => commitKeys(f.key, f.stored)}
						class="{INPUT_CLASS} font-mono"
					></textarea>
					<p class="mt-1 max-w-xl text-xs leading-relaxed text-fg-subtle">{f.help}</p>
				</label>
			{/each}

			<div class="flex flex-wrap items-center gap-2 border-t border-border pt-3 text-xs text-fg-subtle">
				<button
					type="button"
					disabled={retag.isPending || tags.mode === "off" || config.readOnly}
					onclick={() => retag.mutate()}
					class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-bg px-2.5 text-xs font-medium text-fg transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-60"
				>
					<Tags size={13} aria-hidden="true" />
					{retag.isPending ? i18n.common_applying() : i18n.music_tags_retag()}
				</button>
				<span>{i18n.music_tags_retag_help()}</span>
			</div>
		</section>
	{/if}
</div>
