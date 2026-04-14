<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { Mail, Send, Trash2, Clipboard, Link as LinkIcon } from "@lucide/svelte";
	import * as v from "valibot";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { inviteEmail, userRole } from "../../lib/schemas";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import type { Invite, InviteCreated, UserRole } from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import Select from "../forms/Select.svelte";
	import SubmitButton from "../forms/SubmitButton.svelte";
	import Dialog from "../modals/Dialog.svelte";

	const qc = useQueryClient();

	let revoking = $state<number | null>(null);

	const invites = createQuery<Invite[]>(() => ({
		queryKey: ["auth", "invites"],
		queryFn: () => api<Invite[]>("/auth/invites"),
	}));

	let lastCreated = $state<InviteCreated | null>(null);

	const create = createMutation<
		InviteCreated,
		Error,
		{ email: string; role: UserRole }
	>(() => ({
		mutationFn: (body) =>
			api<InviteCreated>("/auth/invites", { method: "POST", body }),
		onSuccess: (resp) => {
			lastCreated = resp;
			form.reset();
			qc.invalidateQueries({ queryKey: ["auth", "invites"] });
			toast.ok("Invite created");
		},
		onError: (err) => toast.err(err.message),
	}));

	const revoke = createMutation<null, Error, number>(() => ({
		mutationFn: (id) =>
			api<null>(`/auth/invites/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["auth", "invites"] });
			toast.ok("Invite revoked");
		},
		onError: (err) => toast.err(err.message),
	}));

	const form = createForm(() => ({
		defaultValues: { email: "", role: "member" as UserRole },
		validators: {
			onChange: v.object({ email: inviteEmail, role: userRole }),
		},
		onSubmit: ({ value }) => create.mutate(value),
	}));

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.ok("Copied");
		} catch {
			toast.err("Clipboard unavailable");
		}
	}

	function rolePill(r: UserRole) {
		switch (r) {
			case "admin":
				return "bg-status-wanted/10 text-status-wanted";
			case "member":
				return "bg-accent/10 text-accent";
			default:
				return "bg-surface text-fg-muted";
		}
	}

	function roleLabel(r: UserRole) {
		return r === "request_only" ? "request only" : r;
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-5">
	<header class="flex items-start gap-3">
		<span
			class="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-accent/10 text-accent"
		>
			<Mail size={16} aria-hidden="true" />
		</span>
		<div>
			<h2 class="text-lg font-semibold text-fg">Invites</h2>
			<p class="mt-0.5 text-sm text-fg-muted">
				Send registration links bound to an email and role. Tokens are
				shown once.
			</p>
		</div>
	</header>

	<form
		class="mt-5 grid gap-3 sm:grid-cols-[1fr_200px_auto] sm:items-end"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<form.Field name="email">
			{#snippet children(field)}
				<TextField
					{field}
					label="Email"
					type="email"
					autocomplete="off"
					placeholder="teammate@example.com"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="role">
			{#snippet children(field)}
				<Select
					label="Role"
					value={field.state.value as UserRole}
					options={[
						{ value: "member", label: "Member" },
						{ value: "request_only", label: "Request only" },
						{ value: "admin", label: "Admin" },
					]}
					onChange={(v) => field.handleChange(v)}
				/>
			{/snippet}
		</form.Field>
		<SubmitButton {form} label="Create invite" pendingLabel="Creating…" />
	</form>

	{#if lastCreated}
		<div
			class="mt-4 rounded-md border border-status-wanted/40 bg-status-wanted/5 p-3 text-xs"
		>
			<p class="mb-2 flex items-center gap-1.5 font-medium text-fg">
				<Send size={12} aria-hidden="true" />
				Invite for {lastCreated.email ?? "anyone"} — copy now, it won't
				be shown again:
			</p>
			<div class="grid gap-2">
				<div>
					<p class="text-fg-muted">Registration link</p>
					<code
						class="mt-1 block break-all rounded bg-bg-deep p-2 font-mono text-fg"
					>
						{lastCreated.url}
					</code>
					<button
						type="button"
						onclick={() => copy(lastCreated!.url)}
						class="mt-1.5 inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-fg-muted hover:bg-surface hover:text-fg"
					>
						<LinkIcon size={12} aria-hidden="true" />
						Copy link
					</button>
				</div>
				<div>
					<p class="text-fg-muted">Raw token</p>
					<code
						class="mt-1 block break-all rounded bg-bg-deep p-2 font-mono text-fg"
					>
						{lastCreated.raw_token}
					</code>
					<button
						type="button"
						onclick={() => copy(lastCreated!.raw_token)}
						class="mt-1.5 inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-fg-muted hover:bg-surface hover:text-fg"
					>
						<Clipboard size={12} aria-hidden="true" />
						Copy token
					</button>
				</div>
			</div>
		</div>
	{/if}

	<div class="mt-5">
		{#if invites.isPending}
			<p class="text-sm text-fg-subtle">Loading…</p>
		{:else if invites.isError}
			<p class="text-sm text-status-failed">
				Failed to load invites: {invites.error?.message}
			</p>
		{:else if (invites.data ?? []).length === 0}
			<p
				class="rounded-md border border-dashed border-border bg-bg-deep/40 px-4 py-3 text-sm text-fg-muted"
			>
				No pending invites.
			</p>
		{:else}
			<ul class="divide-y divide-border rounded-md border border-border">
				{#each invites.data ?? [] as inv (inv.id)}
					{@const expired =
						new Date(inv.expires_at).getTime() < Date.now()}
					{@const used = inv.used_at !== null}
					<li
						class="flex items-start justify-between gap-3 px-4 py-2.5"
					>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-2">
								<p class="truncate text-sm font-medium text-fg">
									{inv.email || "(no email bound)"}
								</p>
								<span
									class="inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide {rolePill(
										inv.role,
									)}"
								>
									{roleLabel(inv.role)}
								</span>
								{#if used}
									<span
										class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
									>
										used
									</span>
								{:else if expired}
									<span
										class="inline-flex items-center rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
									>
										expired
									</span>
								{/if}
							</div>
							<p class="mt-0.5 text-xs text-fg-muted">
								Created {formatDateTime(inv.created_at)} · expires {formatRelative(
									inv.expires_at,
								)}
							</p>
						</div>
						{#if !used}
							<button
								type="button"
								onclick={() => (revoking = inv.id)}
								class="inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs font-medium text-status-failed hover:bg-status-failed/10"
								aria-label="Revoke invite"
							>
								<Trash2 size={14} aria-hidden="true" />
								Revoke
							</button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</section>

<Dialog
	open={revoking !== null}
	title="Revoke this invite?"
	body="The invite link will stop working immediately."
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
