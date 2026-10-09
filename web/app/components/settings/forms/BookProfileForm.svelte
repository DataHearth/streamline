<script lang="ts">
	import TextField from "@components/forms/TextField.svelte";
	import Select from "@components/forms/Select.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { cn } from "@lib/cn";
	import { readOnlyLock } from "@lib/config.svelte";
	import type { AppForm } from "@lib/form";
	import {
		AUDIOBOOK_BITRATES,
		AUDIOBOOK_FORMATS,
		EBOOK_FORMATS,
		bitrateLabel,
		sortFormats,
		type BookProfileValues,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One profile, two slots: the ebook and the audiobook each pick the formats
	// they accept and the one they prefer, the way the book page shows them as
	// two cards. Formats are chips, like the video form's codecs.
	let { form }: { form: AppForm<BookProfileValues> } = $props();

	const lock = readOnlyLock();
	let locked = $derived(lock());
	const values = form.useStore((s) => s.values);

	type Slot = "ebook" | "audiobook";
	const SLOTS: Slot[] = ["ebook", "audiobook"];
	const ALL: Record<Slot, readonly string[]> = { ebook: EBOOK_FORMATS, audiobook: AUDIOBOOK_FORMATS };
	const pickedOf = (v: unknown): string[] => (Array.isArray(v) ? v : []);

	// Same rule as the music tiers: the preferred format is always a ticked one.
	function toggle(slot: Slot, picked: string[], f: string, set: (v: never) => void) {
		const next = sortFormats(ALL[slot], picked.includes(f) ? picked.filter((x) => x !== f) : [...picked, f]);
		set(next as never);
		const key = slot === "ebook" ? "ebook_preferred" : "audiobook_preferred";
		if (next.length && !next.includes(values.current[key])) form.setFieldValue(key, next[0] as never);
	}

	const chip = (on: boolean) =>
		cn(
			"inline-flex h-11 lg:h-9 items-center rounded-full border px-4 font-mono text-[13px] font-medium transition disabled:cursor-not-allowed disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
			on
				? "border-accent-line bg-accent-soft text-accent-text"
				: "border-border bg-bg-elevated text-fg-muted hover:border-border-strong",
		);
	const h4 = "font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";
</script>

<div class="space-y-4">
	<form.Field name="name">
		{#snippet children(field)}
			<TextField {field} label={i18n.common_name()} placeholder="Retail EPUB + M4B" />
		{/snippet}
	</form.Field>

	{#each SLOTS as slot (slot)}
		<section class="space-y-3 rounded-lg border border-border bg-bg-card p-3">
			<div>
				<h4 class={h4}>{slot === "ebook" ? i18n.qp_ebook_formats() : i18n.qp_audiobook_formats()}</h4>
				<p class="mt-1 text-xs leading-relaxed text-fg-muted">
					{slot === "ebook" ? i18n.qp_ebook_formats_help() : i18n.qp_audiobook_formats_help()}
				</p>
			</div>

			<form.Field name={slot === "ebook" ? "ebook_formats" : "audiobook_formats"}>
				{#snippet children(field)}
					{@const picked = pickedOf(field.state.value)}
					<div>
						<span class="sr-only">{slot === "ebook" ? i18n.qp_ebook_formats() : i18n.qp_audiobook_formats()}</span>
						<div class="flex flex-wrap gap-2">
							{#each ALL[slot] as f (f)}
								{@const on = picked.includes(f)}
								<button
									type="button"
									disabled={locked}
									aria-pressed={on}
									onclick={() => toggle(slot, picked, f, field.handleChange)}
									class={chip(on)}
								>
									{f}
								</button>
							{/each}
							<FieldLock {locked} />
						</div>
						{#if picked.length === 0}
							<p class="mt-1.5 text-xs text-status-failed">{i18n.validation_pick_format()}</p>
						{/if}
					</div>
				{/snippet}
			</form.Field>

			<div class="grid gap-3 sm:grid-cols-2">
				<form.Field name={slot === "ebook" ? "ebook_preferred" : "audiobook_preferred"}>
					{#snippet children(field)}
						{@const ticked = (slot === "ebook" ? values.current.ebook_formats : values.current.audiobook_formats) ?? []}
						<Select
							label={i18n.qp_preferred_format()}
							value={field.state.value}
							options={ticked.map((f) => ({ value: f, label: f }))}
							disabled={ticked.length === 0}
							onChange={(v) => field.handleChange(v)}
						/>
					{/snippet}
				</form.Field>
				{#if slot === "audiobook"}
					<form.Field name="min_bitrate">
						{#snippet children(field)}
							<div>
								<Select
									label={i18n.qp_min_bitrate()}
									value={String(field.state.value)}
									options={AUDIOBOOK_BITRATES.map((k) => ({ value: String(k), label: bitrateLabel(k) }))}
									onChange={(v) => field.handleChange(Number(v))}
								/>
								<p class="mt-1 text-xs text-fg-muted">{i18n.qp_min_bitrate_help()}</p>
							</div>
						{/snippet}
					</form.Field>
				{/if}
			</div>
		</section>
	{/each}

	<form.Field name="upgrade_allowed">
		{#snippet children(field)}
			<Checkbox
				name={field.name}
				checked={field.state.value}
				onChange={(v) => field.handleChange(v)}
				label={i18n.quality_allow_upgrades()}
				description={i18n.qp_books_upgrades_help()}
			/>
		{/snippet}
	</form.Field>
</div>
