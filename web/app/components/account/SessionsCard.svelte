<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { MonitorOff } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { parseUA } from "../../lib/ua";
	import type { Session } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";
	import SessionRow from "../shared/SessionRow.svelte";

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
				<SessionRow
					session={s}
					revoking={revoke.isPending}
					onRevoke={() => (pending = s)}
				/>
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
