<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import Checkbox from "../../forms/Checkbox.svelte";
	import { cn } from "../../../lib/cn";
	import { readOnlyLock } from "../../../lib/config.svelte";
	import FieldLock from "../../forms/FieldLock.svelte";
	import { VIDEO_CODECS } from "../../../lib/media-info";
	import type { Resolution } from "../../../lib/types";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	type Values = {
		name: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		upgrade_allowed: boolean;
		allowed_codecs: string[];
	};

	type Props = { form: FormApi<Values, undefined> };
	let { form }: Props = $props();

	const RESOLUTIONS: Resolution[] = ["720p", "1080p", "2160p"];

	// Labels are human, stored values are ffprobe's — mapped once, here, so the
	// codec check on the backend needs no table of its own.
	const lock = readOnlyLock();
	let locked = $derived(lock());

	function toggleCodec(current: string[], value: string): string[] {
		return current.includes(value)
			? current.filter((c) => c !== value)
			: [...current, value];
	}
</script>

<div class="space-y-4">
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
</div>
