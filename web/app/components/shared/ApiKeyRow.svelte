<script lang="ts">
	import { Key, Trash2 } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { formatDateTime, formatRelative } from "@lib/dates";
	import type { ApiKey } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		apiKey,
		revoking = false,
		onRevoke,
	}: {
		apiKey: ApiKey;
		revoking?: boolean;
		onRevoke: () => void;
	} = $props();

	// Read once per render: an expired key authenticates nothing, and stays
	// listed only until the purge job drops it a week later.
	let expired = $derived(
		!!apiKey.expires_at && new Date(apiKey.expires_at).getTime() <= Date.now(),
	);
</script>

<li class="flex items-start gap-3.5 px-5 py-3.5">
	<div
		class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted"
		aria-hidden="true"
	>
		<Key size={18} />
	</div>
	<div class="min-w-0 flex-1">
		<div class="flex min-w-0 items-center gap-2">
			<p
				class={cn(
					"truncate text-sm font-semibold text-fg",
					expired && "text-fg-muted line-through",
				)}
			>
				{apiKey.name}
			</p>
			<span
				class={cn(
					"shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium",
					apiKey.scope === "read"
						? "bg-bg-card text-fg-muted"
						: "bg-status-wanted/10 text-status-wanted",
				)}
				title={apiKey.scope === "read"
					? i18n.apikey_scope_read_hint()
					: i18n.apikey_scope_full_hint()}
			>
				{apiKey.scope === "read"
					? i18n.apikey_scope_read()
					: i18n.apikey_scope_full()}
			</span>
		</div>
		<dl class="mt-1 flex flex-wrap gap-x-3.5 gap-y-0.5 text-xs text-fg-muted">
			<div class="flex items-center gap-1">
				<dt class="text-fg-subtle">{i18n.apikey_created()}</dt>
				<dd title={formatDateTime(apiKey.created_at)}
					>{formatRelative(apiKey.created_at)}</dd
				>
			</div>
			<div class="flex items-center gap-1">
				<dt class="text-fg-subtle">
					{expired ? i18n.apikey_expired() : i18n.apikey_expires()}
				</dt>
				<dd
					class:text-fg-subtle={!apiKey.expires_at}
					class:text-status-failed={expired}
					title={apiKey.expires_at
						? formatDateTime(apiKey.expires_at)
						: undefined}
				>
					{apiKey.expires_at ? formatRelative(apiKey.expires_at) : i18n.lc_never()}
				</dd>
			</div>
			<div class="flex items-center gap-1">
				<dt class="text-fg-subtle">{i18n.apikey_last_used()}</dt>
				<dd
					class:text-fg-subtle={!apiKey.last_used_at}
					title={apiKey.last_used_at
						? formatDateTime(apiKey.last_used_at)
						: undefined}
				>
					{apiKey.last_used_at ? formatRelative(apiKey.last_used_at) : i18n.lc_never()}
				</dd>
			</div>
		</dl>
	</div>
	<button
		type="button"
		disabled={revoking}
		onclick={onRevoke}
		class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed lg:h-8 lg:min-h-0 transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:text-fg-faint disabled:hover:bg-transparent"
		aria-label={i18n.a11y_revoke_api_key({ name: apiKey.name })}
	>
		<Trash2 size={14} aria-hidden="true" />
		{i18n.common_revoke()}
	</button>
</li>
