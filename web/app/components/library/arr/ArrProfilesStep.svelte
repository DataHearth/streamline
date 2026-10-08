<script lang="ts">
	import type { SvelteMap } from "svelte/reactivity";
	import { createQuery } from "@tanstack/svelte-query";
	import { ChevronDown } from "@lucide/svelte";
	import { api } from "@lib/api";
	import {
		appLabel,
		defaultProfileChoice,
		type ProfileChoice,
	} from "@lib/arr-import";
	import { cn } from "@lib/cn";
	import type {
		ArrApp,
		ArrProfileTranslation,
		QualityProfile,
	} from "@lib/types";
	import Select from "@components/forms/Select.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		app: ArrApp;
		profiles: ArrProfileTranslation[];
		// Owned by the wizard so the choices survive stepping back and forth.
		choices: SvelteMap<number, ProfileChoice>;
	};

	let { app, profiles, choices }: Props = $props();

	let label = $derived(appLabel(app));

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
	}));

	// Select options carry strings, so a choice is encoded as one: an existing
	// profile's name follows a prefix no other value uses.
	type Encoded = "create" | "default" | `existing:${string}`;

	function encode(c: ProfileChoice): Encoded {
		return c.kind === "existing" ? `existing:${c.name}` : c.kind;
	}

	function decode(v: Encoded): ProfileChoice {
		if (v === "create" || v === "default") return { kind: v };
		return { kind: "existing", name: v.slice("existing:".length) };
	}

	function optionsFor(t: ArrProfileTranslation) {
		const names = (qpQuery.data ?? []).map((p) => p.name);
		// The profile holding the source's name is offered even before the list
		// has loaded — it is the default choice for this row.
		if (t.existing && !names.includes(t.existing)) names.unshift(t.existing);
		const opts: { value: Encoded; label: string }[] = [];
		// Creating it would collide with the streamline profile already holding
		// that name, so the option is not offered at all.
		if (!t.existing) {
			opts.push({ value: "create", label: i18n.arr_profile_create({ name: t.name }) });
		}
		for (const name of names) {
			opts.push({ value: `existing:${name}`, label: i18n.arr_profile_use({ name }) });
		}
		opts.push({ value: "default", label: i18n.arr_profile_default() });
		return opts;
	}

	let open = $state<Record<number, boolean>>({});
</script>

<div class="space-y-4">
	<p class="text-sm text-fg-muted">{i18n.arr_profiles_intro({ app: label })}</p>

	<ul class="space-y-3">
		{#each profiles as t (t.id)}
			{@const choice = choices.get(t.id) ?? defaultProfileChoice(t)}
			{@const formats = t.translation.formats?.length ?? 0}
			{@const regionId = `arr-profile-${t.id}-details`}
			<li class="space-y-3 rounded-md border border-border bg-bg-elevated p-3">
				<div class="min-w-0">
					<p class="truncate text-sm font-semibold text-fg" title={t.name}>
						{t.name}
					</p>
					<p class="mt-0.5 text-xs text-fg-muted">
						{t.in_use === 1
							? i18n.arr_profile_in_use_one({ count: t.in_use })
							: i18n.arr_profile_in_use_other({ count: t.in_use })}
					</p>
				</div>

				<Select
					ariaLabel={t.name}
					value={encode(choice)}
					options={optionsFor(t)}
					onChange={(v) => choices.set(t.id, decode(v))}
				/>

				<div>
					<button
						type="button"
						aria-expanded={open[t.id] ?? false}
						aria-controls={regionId}
						onclick={() => (open[t.id] = !open[t.id])}
						class="inline-flex items-center gap-1 rounded text-xs font-medium text-fg-muted transition hover:text-fg"
					>
						<ChevronDown
							size={14}
							aria-hidden="true"
							class={cn(
								"transition-transform duration-150",
								open[t.id] && "rotate-180",
							)}
						/>
						{i18n.arr_profile_details()}
					</button>
					<div
						id={regionId}
						hidden={!open[t.id]}
						class="mt-2 space-y-1 border-l-2 border-border pl-3 text-xs text-fg-muted"
					>
						<p>
							{i18n.arr_profile_resolution({
								min:
									t.translation.min_resolution ??
									t.translation.preferred_resolution,
								preferred: t.translation.preferred_resolution,
							})}
						</p>
						<p>
							{t.translation.upgrade_allowed
								? i18n.arr_profile_upgrades_on()
								: i18n.arr_profile_upgrades_off()}
						</p>
						{#if formats > 0}
							<p>
								{formats === 1
									? i18n.arr_profile_formats_one({ count: formats })
									: i18n.arr_profile_formats_other({ count: formats })}
							</p>
						{/if}
						{#if t.notes.length > 0}
							<ul class="list-disc space-y-0.5 pl-4 text-fg-subtle">
								{#each t.notes as note, i (i)}
									<li class="break-words">{note}</li>
								{/each}
							</ul>
						{/if}
					</div>
				</div>
			</li>
		{/each}
	</ul>
</div>
