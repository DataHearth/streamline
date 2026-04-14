<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Monitor, MonitorOff, Trash2 } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import { parseUA } from "../../lib/ua";
	import type { Session } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";

	let {
		userId,
		sessions,
	}: {
		userId: number;
		sessions: Session[];
	} = $props();

	let revoking = $state<number | null>(null);

	const qc = useQueryClient();

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/users/${userId}/sessions/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", userId] });
			toast.ok("Session revoked");
		},
		onError: (err) => toast.err(err.message),
	}));
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header
		class="flex items-center justify-between border-b border-border px-5 py-3.5"
	>
		<div>
			<h3 class="text-base font-semibold text-fg">Active sessions</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				{sessions.length}
				{sessions.length === 1 ? "device" : "devices"} signed in
			</p>
		</div>
	</header>

	{#if sessions.length === 0}
		<div class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted">
			<MonitorOff size={16} aria-hidden="true" />
			<span>No active sessions.</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each sessions as s (s.id)}
				{@const ua = parseUA(s.user_agent)}
				<li class="flex items-start gap-3.5 px-5 py-3.5">
					<div
						class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted"
						aria-hidden="true"
					>
						<Monitor size={18} />
					</div>
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-semibold text-fg">
							{ua.browser} · {ua.os}
						</p>
						<dl
							class="mt-1 flex flex-wrap gap-x-3.5 gap-y-0.5 text-xs text-fg-muted"
						>
							{#if s.ip}
								<div class="flex items-center gap-1">
									<dt class="text-fg-subtle">IP</dt>
									<dd class="font-mono">{s.ip}</dd>
								</div>
							{/if}
							{#if s.last_seen_at}
								<div class="flex items-center gap-1">
									<dt class="text-fg-subtle">last seen</dt>
									<dd
										title={formatDateTime(s.last_seen_at)}
									>{formatRelative(s.last_seen_at)}</dd>
								</div>
							{/if}
							<div class="flex items-center gap-1">
								<dt class="text-fg-subtle">expires</dt>
								<dd title={formatDateTime(s.expires_at)}
									>{formatRelative(s.expires_at)}</dd
								>
							</div>
						</dl>
					</div>
					<button
						type="button"
						disabled={revoke.isPending}
						onclick={() => (revoking = s.id)}
						class="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:opacity-40"
						aria-label="Revoke session"
					>
						<Trash2 size={14} aria-hidden="true" />
						Revoke
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<Dialog
	open={revoking !== null}
	title="Revoke this session?"
	body="The device will be signed out and must log in again."
	onClose={() => (revoking = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Revoke",
			variant: "danger",
			onClick: () => revoking !== null && revoke.mutate(revoking),
		},
	]}
/>
