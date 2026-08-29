<script lang="ts">
	import { Monitor, Trash2 } from "@lucide/svelte";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import { parseUA } from "../../lib/ua";
	import type { Session } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		session,
		revoking = false,
		onRevoke,
	}: {
		session: Session;
		revoking?: boolean;
		onRevoke: () => void;
	} = $props();

	let ua = $derived(parseUA(session.user_agent));
</script>

<li class="flex items-start gap-3.5 px-5 py-3.5">
	<div
		class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted"
		aria-hidden="true"
	>
		<Monitor size={18} />
	</div>
	<div class="min-w-0 flex-1">
		<div class="flex flex-wrap items-center gap-2">
			<span class="truncate text-sm font-semibold text-fg">
				{ua.browser} · {ua.os}
			</span>
			{#if session.is_current}
				<span
					class="inline-flex items-center gap-1 rounded-full bg-status-available/12 px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wide text-status-available"
				>
					<span class="h-1.5 w-1.5 rounded-full bg-current"></span>
					{i18n.session_this_device()}
				</span>
			{/if}
		</div>
		<dl class="mt-1 flex flex-wrap gap-x-3.5 gap-y-0.5 text-xs text-fg-muted">
			{#if session.ip}
				<div class="flex items-center gap-1">
					<dt class="text-fg-subtle">IP</dt>
					<dd class="font-mono">{session.ip}</dd>
				</div>
			{/if}
			{#if session.last_seen_at}
				<div class="flex items-center gap-1">
					<dt class="text-fg-subtle">last seen</dt>
					<dd title={formatDateTime(session.last_seen_at)}
						>{formatRelative(session.last_seen_at)}</dd
					>
				</div>
			{/if}
			<div class="flex items-center gap-1">
				<dt class="text-fg-subtle">expires</dt>
				<dd title={formatDateTime(session.expires_at)}
					>{formatRelative(session.expires_at)}</dd
				>
			</div>
		</dl>
	</div>
	<button
		type="button"
		disabled={session.is_current || revoking}
		onclick={onRevoke}
		class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed lg:h-8 lg:min-h-0 transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:text-fg-faint disabled:hover:bg-transparent"
		aria-label={i18n.action_revoke_session()}
	>
		<Trash2 size={14} aria-hidden="true" />
		{i18n.common_revoke()}
	</button>
</li>
