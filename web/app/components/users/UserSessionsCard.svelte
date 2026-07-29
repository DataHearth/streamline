<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { MonitorOff } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { Session } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";
	import SessionRow from "../shared/SessionRow.svelte";

	let {
		userId,
		sessions,
	}: {
		userId: number;
		sessions: Session[];
	} = $props();

	let pending = $state<Session | null>(null);

	const qc = useQueryClient();

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/users/${userId}/sessions/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", userId] });
			toast.ok("Session revoked");
			pending = null;
		},
		onError: (err) => {
			toast.err(err.message);
			pending = null;
		},
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
			<span>No sessions on record yet.</span>
		</div>
	{:else}
		<ul class="max-h-[26rem] divide-y divide-border overflow-y-auto">
			{#each sessions as s (s.id)}
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
	title="Revoke this session?"
	body="The device will be signed out and must log in again."
	onClose={() => {
		if (!revoke.isPending) pending = null;
	}}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Revoke",
			variant: "danger",
			dismiss: false,
			pending: revoke.isPending,
			onClick: () => pending && revoke.mutate(pending.id),
		},
	]}
/>
