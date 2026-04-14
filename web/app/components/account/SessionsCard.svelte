<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Monitor, MonitorOff, Trash2 } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import { parseUA } from "../../lib/ua";
	import type { Session } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";

	const qc = useQueryClient();

	const sessions = createQuery<Session[]>(() => ({
		queryKey: ["auth", "me", "sessions"],
		queryFn: () => api<Session[]>("/auth/me/sessions"),
	}));

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/auth/me/sessions/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["auth", "me", "sessions"] });
			toast.ok("Session revoked");
			pending = null;
		},
		onError: (err) => {
			toast.err(err.message);
			pending = null;
		},
	}));

	let items = $derived(sessions.data ?? []);
	let pending = $state<Session | null>(null);

	let pendingLabel = $derived.by(() => {
		if (!pending) return "";
		const ua = parseUA(pending.user_agent);
		return `${ua.browser} · ${ua.os}`;
	});
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header
		class="flex items-center justify-between border-b border-border px-5 py-3.5"
	>
		<div>
			<h3 class="text-base font-semibold text-fg">Active sessions</h3>
			<p class="mt-0.5 text-xs text-fg-muted">
				{items.length}
				{items.length === 1 ? "device" : "devices"} signed in
			</p>
		</div>
	</header>

	{#if sessions.isPending}
		<p class="px-5 py-6 text-sm text-fg-subtle">Loading…</p>
	{:else if sessions.isError}
		<p class="px-5 py-6 text-sm text-status-failed">
			Failed to load: {sessions.error?.message}
		</p>
	{:else if items.length === 0}
		<div
			class="flex items-center gap-2 px-5 py-6 text-sm text-fg-muted"
		>
			<MonitorOff size={16} aria-hidden="true" />
			<span>No sessions on record yet.</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each items as s (s.id)}
				{@const ua = parseUA(s.user_agent)}
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
							{#if s.is_current}
								<span
									class="inline-flex items-center gap-1 rounded-full bg-status-available/12 px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wide text-status-available"
								>
									<span
										class="h-1.5 w-1.5 rounded-full bg-current"
									></span>
									This device
								</span>
							{/if}
						</div>
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
						disabled={s.is_current || revoke.isPending}
						onclick={() => {
							pending = s;
						}}
						class="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed transition hover:bg-status-failed/10 disabled:cursor-not-allowed disabled:text-fg-faint disabled:hover:bg-transparent"
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
	open={pending !== null}
	title="Sign out this device?"
	body={pendingLabel
		? `Sign out ${pendingLabel}? It will need to log in again to access your account.`
		: "Sign out this session?"}
	onClose={() => {
		if (!revoke.isPending) pending = null;
	}}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Sign out",
			variant: "danger",
			dismiss: false,
			pending: revoke.isPending,
			onClick: () => pending && revoke.mutate(pending.id),
		},
	]}
/>
