<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { ApiError, api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { userExtensionCardPath, userExtensionsKey, type ExtensionUserCard } from "@lib/extensions";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One extension's per-account settings, drawn from the fields it declares.
	// Save-per-control: each change sends every value, since the server checks
	// the whole card.
	let { userId, card }: { userId: number; card: ExtensionUserCard } = $props();

	const qc = useQueryClient();

	let drafts = $state<Record<string, string | undefined>>({});
	// A refused save leaves the native checkbox in the state the click put it
	// in, since its `checked` prop never changed: re-creating the grid puts
	// every box back on the stored values.
	let rev = $state(0);

	const save = createMutation<unknown, Error, Record<string, boolean | number>>(() => ({
		mutationFn: (values) =>
			api(userExtensionCardPath(userId, card.extension, card.card), { method: "PUT", body: { values } }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: userExtensionsKey(userId) });
			toast.ok(i18n.common_saved());
		},
		onError: (err) => {
			rev++;
			toast.err(err instanceof ApiError && err.status === 422 && err.message ? err.message : errorText(err));
		},
		onSettled: () => (drafts = {}),
	}));

	function commit(el: HTMLInputElement, id: string, min: number, max: number) {
		const raw = drafts[id]?.trim();
		if (raw === undefined) return;
		const n = Number(raw);
		if (raw === "" || !Number.isFinite(n)) {
			drafts[id] = undefined;
			el.value = String(card.values[id] ?? min);
			return;
		}
		const value = Math.min(max, Math.max(min, Math.round(n)));
		drafts[id] = String(value);
		if (value === card.values[id]) {
			drafts[id] = undefined;
			return;
		}
		save.mutate({ ...card.values, [id]: value });
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-5">
	<header class="mb-4">
		<h3 class="text-base font-semibold text-fg">{card.title}</h3>
		<p class="mt-0.5 text-xs text-fg-muted">{i18n.extensions_user_card_origin({ extension: card.extension })}</p>
	</header>

	{#key rev}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			{#each card.fields as field (field.id)}
				{#if field.kind === "switch"}
					<Checkbox
						checked={card.values[field.id] === true}
						label={field.label}
						disabled={save.isPending}
						onChange={(v) => save.mutate({ ...card.values, [field.id]: v })}
					/>
				{/if}
			{/each}
		</div>
	{/key}

	{#each card.fields as field (field.id)}
		{#if field.kind === "number"}
			<label class="mt-5 flex flex-wrap items-center gap-x-3 gap-y-1.5">
				<span class="text-sm text-fg">{field.label}</span>
				<input
					type="number"
					inputmode="numeric"
					min={field.min}
					max={field.max}
					step="1"
					disabled={save.isPending}
					value={drafts[field.id] ?? String(card.values[field.id] ?? field.min)}
					oninput={(e) => (drafts[field.id] = e.currentTarget.value)}
					onchange={(e) => commit(e.currentTarget, field.id, field.min, field.max)}
					class="w-24 rounded-md border border-border bg-bg px-3 py-1.5 text-sm text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-60"
				/>
				<span class="text-xs text-fg-muted">{i18n.extensions_user_range({ min: field.min, max: field.max })}</span>
			</label>
		{/if}
	{/each}
</section>
