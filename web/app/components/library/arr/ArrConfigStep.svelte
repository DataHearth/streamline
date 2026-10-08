<script lang="ts" module>
	// One checklist row's state: whether it is ticked, and the secret the
	// source instance would not hand back.
	export type ConfigPick = { on: boolean; secret: string };
</script>

<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Plus } from "@lucide/svelte";
	import { errorText } from "@lib/api";
	import {
		appLabel,
		applySourceConfig,
		type ApplySourceConfigRequest,
	} from "@lib/arr-import";
	import { INPUT_CLASS } from "@lib/form";
	import type {
		ApplySourceConfigResult,
		ArrApp,
		ArrClientOption,
		ArrConfigSelection,
		ArrIndexerOption,
	} from "@lib/types";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import ArrConfigTest from "./ArrConfigTest.svelte";
	import ArrPill from "./ArrPill.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		app: ArrApp;
		url: string;
		apiKey: string;
		indexers: ArrIndexerOption[];
		clients: ArrClientOption[];
		indexerPicks: Record<string, ConfigPick>;
		clientPicks: Record<string, ConfigPick>;
		applied: ApplySourceConfigResult | null;
	};

	let {
		app,
		url,
		apiKey,
		indexers,
		clients,
		indexerPicks = $bindable(),
		clientPicks = $bindable(),
		applied = $bindable(),
	}: Props = $props();

	let label = $derived(appLabel(app));

	const qc = useQueryClient();

	type List = "indexers" | "download_clients";

	// The two option shapes, folded into what a checklist row needs.
	type Row = {
		name: string;
		// Brand or protocol name, shown literally.
		type: string | null;
		// Why the row cannot be picked; null when it can.
		blocked: string | null;
		needsSecret: boolean;
		collapses: number;
	};

	const INDEXER_TYPES = { torznab: "Torznab", prowlarr: "Prowlarr" } as const;
	const CLIENT_TYPES = {
		qbittorrent: "qBittorrent",
		transmission: "Transmission",
		deluge: "Deluge",
	} as const;

	function blockedReason(o: {
		reason?: string;
		conflict: boolean;
		unsupported: boolean;
	}): string | null {
		if (o.unsupported) return o.reason ?? "";
		if (o.conflict) return i18n.arr_config_conflict();
		return null;
	}

	// The type line only earns its place when it says something the name does
	// not: a Prowlarr entry is named "Prowlarr", and an *arr's default client
	// name is its type.
	function typeLine(name: string, type: string): string | null {
		return name.toLowerCase() === type.toLowerCase() ? null : type;
	}

	let indexerRows = $derived<Row[]>(
		indexers.map((o) => ({
			name: o.name,
			type:
				o.kind === "unsupported" ? null : typeLine(o.name, INDEXER_TYPES[o.kind]),
			blocked: blockedReason({ ...o, unsupported: o.kind === "unsupported" }),
			needsSecret: o.needs_secret,
			collapses: o.kind === "prowlarr" ? o.collapses : 0,
		})),
	);

	let clientRows = $derived<Row[]>(
		clients.map((o) => ({
			name: o.name,
			type:
				o.client_type === "unsupported"
					? null
					: typeLine(o.name, CLIENT_TYPES[o.client_type]),
			blocked: blockedReason({
				...o,
				unsupported: o.client_type === "unsupported",
			}),
			needsSecret: o.needs_secret,
			collapses: 0,
		})),
	);

	function picksOf(list: List): Record<string, ConfigPick> {
		return list === "indexers" ? indexerPicks : clientPicks;
	}

	function isAdded(list: List, name: string): boolean {
		return applied?.[list].includes(name) ?? false;
	}

	function pick(list: List, name: string): ConfigPick {
		return picksOf(list)[name] ?? { on: false, secret: "" };
	}

	function setPick(list: List, name: string, p: ConfigPick) {
		if (list === "indexers") indexerPicks[name] = p;
		else clientPicks[name] = p;
	}

	function selections(list: List, rows: Row[]): ArrConfigSelection[] {
		return rows
			.filter((r) => r.blocked === null && !isAdded(list, r.name) && pick(list, r.name).on)
			.map((r) =>
				r.needsSecret
					? { name: r.name, secret: pick(list, r.name).secret }
					: { name: r.name },
			);
	}

	let picked = $derived({
		indexers: selections("indexers", indexerRows),
		download_clients: selections("download_clients", clientRows),
	});
	let pickedCount = $derived(
		picked.indexers.length + picked.download_clients.length,
	);
	// A ticked row that needs a secret cannot be added without one.
	let missingSecret = $derived(
		[...indexerRows.map((r) => ["indexers", r] as const),
			...clientRows.map((r) => ["download_clients", r] as const)].some(
			([list, r]) =>
				r.needsSecret &&
				r.blocked === null &&
				!isAdded(list, r.name) &&
				pick(list, r.name).on &&
				pick(list, r.name).secret === "",
		),
	);

	const apply = createMutation<
		ApplySourceConfigResult,
		Error,
		ApplySourceConfigRequest
	>(() => ({
		mutationFn: applySourceConfig,
		onSuccess: (res) => {
			applied = {
				indexers: [...(applied?.indexers ?? []), ...res.indexers],
				download_clients: [
					...(applied?.download_clients ?? []),
					...res.download_clients,
				],
			};
			// The secrets have done their job; nothing keeps them past the add.
			for (const n of res.indexers) delete indexerPicks[n];
			for (const n of res.download_clients) delete clientPicks[n];
			qc.invalidateQueries({ queryKey: ["indexers"] });
			qc.invalidateQueries({ queryKey: ["download-clients"] });
		},
	}));

	function submit() {
		apply.mutate({
			app,
			url,
			api_key: apiKey,
			indexers: picked.indexers,
			download_clients: picked.download_clients,
		});
	}

	function testEndpoint(list: List, name: string): string {
		const base = list === "indexers" ? "/indexers" : "/download-clients";
		return `${base}/${encodeURIComponent(name)}/test`;
	}
</script>

{#snippet section(list: List, heading: string, rows: Row[])}
	<section class="space-y-2">
		<h4 class="text-sm font-semibold text-fg">{heading}</h4>
		{#if rows.length === 0}
			<p class="text-xs text-fg-subtle">{i18n.arr_config_empty()}</p>
		{:else}
			<ul class="space-y-2">
				{#each rows as r (r.name)}
					{@const added = isAdded(list, r.name)}
					{@const p = pick(list, r.name)}
					{@const secretId = `arr-secret-${list}-${r.name}`}
					<li class="space-y-2 rounded-md border border-border bg-bg-elevated p-3">
						<div class="flex min-w-0 items-start justify-between gap-3">
							<Checkbox
								class="min-w-0 flex-1"
								checked={added || p.on}
								disabled={added || r.blocked !== null || apply.isPending}
								onChange={(v) => setPick(list, r.name, { ...p, on: v })}
							>
								<span class="min-w-0 flex-1">
									<span class="block truncate text-sm font-medium text-fg" title={r.name}
										>{r.name}</span
									>
									{#if r.type}
										<span class="block text-xs text-fg-subtle">{r.type}</span>
									{/if}
									{#if r.collapses > 0}
										<span class="block text-xs text-fg-muted">
											{r.collapses === 1
												? i18n.arr_config_collapses_one({ count: r.collapses })
												: i18n.arr_config_collapses_other({ count: r.collapses })}
										</span>
									{/if}
									{#if r.blocked}
										<span class="block text-xs break-words text-fg-muted">{r.blocked}</span>
									{/if}
								</span>
							</Checkbox>
							{#if added}
								<ArrPill tone="ok" label={i18n.arr_config_added()} />
							{/if}
						</div>

						{#if p.on && r.needsSecret && !added && r.blocked === null}
							<div class="pl-6.5">
								<label for={secretId} class="mb-1 block text-sm font-medium text-fg"
									>{i18n.arr_config_secret()}</label
								>
								<input
									id={secretId}
									type="password"
									autocomplete="off"
									value={p.secret}
									disabled={apply.isPending}
									oninput={(e) =>
										setPick(list, r.name, {
											on: true,
											secret: (e.currentTarget as HTMLInputElement).value,
										})}
									class={INPUT_CLASS}
								/>
								<p class="mt-1 text-xs text-fg-muted">
									{i18n.arr_config_secret_help({ app: label })}
								</p>
							</div>
						{/if}

						{#if added}
							<div class="pl-6.5">
								<ArrConfigTest
									endpoint={testEndpoint(list, r.name)}
									queryKey={list === "indexers" ? "indexers" : "download-clients"}
								/>
							</div>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</section>
{/snippet}

<div class="space-y-5">
	<p class="text-sm text-fg-muted">{i18n.arr_config_intro({ app: label })}</p>

	{@render section("indexers", i18n.arr_config_indexers(), indexerRows)}
	{@render section("download_clients", i18n.arr_config_clients(), clientRows)}

	{#if apply.isError}
		<p role="alert" class="text-sm break-words text-status-failed">
			{errorText(apply.error)}
		</p>
	{/if}

	{#if indexerRows.length + clientRows.length > 0}
		<div class="flex justify-end">
			<button
				type="button"
				disabled={pickedCount === 0 || missingSecret || apply.isPending}
				onclick={submit}
				class="inline-flex min-h-11 items-center gap-1.5 rounded-md border border-border bg-bg-base px-3 text-sm font-medium text-fg transition hover:border-border-strong disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0"
			>
				<Plus size={14} aria-hidden="true" />
				{apply.isPending ? i18n.arr_config_applying() : i18n.arr_config_apply()}
			</button>
		</div>
	{/if}
</div>
