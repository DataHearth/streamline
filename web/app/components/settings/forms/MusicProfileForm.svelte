<script lang="ts">
	import TextField from "@components/forms/TextField.svelte";
	import Select from "@components/forms/Select.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { readOnlyLock } from "@lib/config.svelte";
	import type { AppForm } from "@lib/form";
	import {
		MUSIC_TIERS,
		sortTiers,
		tierAlbumSize,
		tierFormats,
		tierLabel,
		type MusicProfileValues,
		type MusicTier,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A music profile is a list of tiers, best first, each saying what it is and
	// what an album costs on disk at that tier — the two things the choice turns
	// on. Ticking decides what may be grabbed; Preferred is where upgrading stops.
	let { form }: { form: AppForm<MusicProfileValues> } = $props();

	const lock = readOnlyLock();
	let locked = $derived(lock());
	const values = form.useStore((s) => s.values);
	let preferred = $derived(values.current.preferred);

	// Preferred always names a ticked tier: unticking it moves the mark to the
	// best tier still ticked, and the first tick after an empty list takes it.
	function toggle(picked: MusicTier[], t: MusicTier, on: boolean, set: (v: MusicTier[]) => void) {
		const next = sortTiers(on ? [...picked, t] : picked.filter((x) => x !== t));
		set(next);
		const best = next[0];
		if (best && !next.includes(values.current.preferred)) form.setFieldValue("preferred", best);
	}
</script>

<div class="space-y-4">
	<form.Field name="name">
		{#snippet children(field)}
			<TextField {field} label={i18n.common_name()} placeholder="Lossless" />
		{/snippet}
	</form.Field>

	<form.Field name="tiers">
		{#snippet children(field)}
			{@const picked = field.state.value ?? []}
			<div>
				<span class="flex items-center gap-1.5 text-sm font-medium text-fg">
					{i18n.qp_tiers()}
					<FieldLock {locked} />
				</span>
				<p class="mt-0.5 text-xs leading-relaxed text-fg-muted">{i18n.qp_tiers_help()}</p>
				<ul class="mt-2.5 divide-y divide-border overflow-hidden rounded-lg border border-border bg-bg-card">
					{#each MUSIC_TIERS as t (t)}
						{@const on = picked.includes(t)}
						<li class="px-3 py-2.5">
							<Checkbox checked={on} disabled={locked} onChange={(v) => toggle(picked, t, v, field.handleChange)}>
								<span class="flex min-w-0 flex-1 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
									<span class="min-w-0">
										<span class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium {on ? 'text-fg' : 'text-fg-muted'}">
											{tierLabel(t)}
											{#if on && preferred === t}
												<span
													class="inline-flex items-center rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent"
												>
													{i18n.qp_preferred_badge()}
												</span>
											{/if}
										</span>
										<span class="mt-0.5 block font-mono text-[11px] text-fg-subtle">{tierFormats(t)}</span>
									</span>
									<span class="shrink-0 font-mono text-[11px] text-fg-faint">{tierAlbumSize(t)}</span>
								</span>
							</Checkbox>
						</li>
					{/each}
				</ul>
				{#if field.state.meta.errors.length > 0}
					<p class="mt-1.5 text-xs text-status-failed">{i18n.qp_tiers_none()}</p>
				{/if}
			</div>
		{/snippet}
	</form.Field>

	<form.Field name="preferred">
		{#snippet children(field)}
			{@const ticked = sortTiers(values.current.tiers ?? [])}
			<div class="sm:max-w-[50%] sm:pr-1.5">
				<Select
					label={i18n.quality_preferred()}
					value={field.state.value}
					options={ticked.map((t) => ({ value: t, label: tierLabel(t) }))}
					disabled={ticked.length === 0}
					onChange={(v) => field.handleChange(v)}
				/>
				<p class="mt-1 text-xs text-fg-muted">{i18n.qp_preferred_tier_help()}</p>
			</div>
		{/snippet}
	</form.Field>

	<form.Field name="upgrade_allowed">
		{#snippet children(field)}
			<Checkbox
				name={field.name}
				checked={field.state.value}
				onChange={(v) => field.handleChange(v)}
				label={i18n.quality_allow_upgrades()}
				description={i18n.qp_music_upgrades_help()}
			/>
		{/snippet}
	</form.Field>
</div>
