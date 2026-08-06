<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { goto } from "@roxi/routify";
	import { onMount } from "svelte";
	import { KeyRound, Unlock, Trash2 } from "@lucide/svelte";
	import * as v from "valibot";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { password } from "../../lib/schemas";
	import type { User } from "../../lib/types";
	import Dialog from "../modals/Dialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { user, isSelf }: { user: User; isSelf: boolean } = $props();

	const qc = useQueryClient();

	// goto is a derived store; get(goto) from a mutation callback re-subscribes
	// on a falsy fragment and throws "derived() expects stores as input".
	// Snapshot the navigate fn at mount instead.
	let navigate: (path: string) => void = () => {};
	onMount(() => goto.subscribe((fn) => (navigate = fn)));

	let confirmDelete = $state(false);

	let locked = $derived.by(() => {
		if (!user.locked_until) return false;
		return new Date(user.locked_until).getTime() > Date.now();
	});

	const resetPw = createMutation<null, Error, { new_password: string }>(() => ({
		mutationFn: (body) =>
			api<null>(`/users/${user.id}/password-reset`, {
				method: "POST",
				body,
			}),
		onSuccess: () => {
			form.reset();
			qc.invalidateQueries({ queryKey: ["user", user.id] });
			toast.ok("Password reset; sessions revoked");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const unlock = createMutation<null, Error, void>(() => ({
		mutationFn: () =>
			api<null>(`/users/${user.id}/unlock`, { method: "POST" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", user.id] });
			toast.ok("Lockout cleared");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const del = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>(`/users/${user.id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["users"] });
			toast.ok("User deleted");
			navigate("/settings/users");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: { new_password: "" },
		validators: { onChange: v.object({ new_password: password }) },
		onSubmit: ({ value }) => resetPw.mutate(value),
	}));

	function onDelete() {
		confirmDelete = true;
	}
</script>

<section
	class="overflow-hidden rounded-lg border border-status-failed/30 bg-status-failed/5"
>
	<div class="divide-y divide-status-failed/15">
		<div
			class="flex flex-col gap-4 p-5 md:flex-row md:items-center md:justify-between md:p-6"
		>
			<div class="flex items-start gap-3.5">
				<span
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-status-failed/12 text-status-failed"
					aria-hidden="true"
				>
					<KeyRound size={20} />
				</span>
				<div class="min-w-0 max-w-md">
					<h3 class="text-sm font-semibold text-fg">{i18n.users_reset_password()}</h3>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.users_reset_help()}
					</p>
				</div>
			</div>
			<form
				class="flex w-full gap-2 md:w-auto md:items-center"
				onsubmit={(e) => {
					e.preventDefault();
					form.handleSubmit();
				}}
			>
				<form.Field name="new_password">
					{#snippet children(field)}
						<input
							type="password"
							name={field.name}
							value={field.state.value}
							oninput={(e) =>
								field.handleChange(
									(e.currentTarget as HTMLInputElement).value,
								)}
							placeholder={i18n.account_new_password()}
							autocomplete="new-password"
							class="h-10 w-full min-w-0 rounded-md border border-status-failed/30 bg-bg px-3 text-sm text-fg placeholder:text-fg-faint focus-visible:border-status-failed focus-visible:outline-2 focus-visible:outline-status-failed md:w-56"
						/>
					{/snippet}
				</form.Field>
				<button
					type="submit"
					disabled={!form.state.canSubmit || form.state.isSubmitting}
					class="inline-flex h-10 shrink-0 items-center gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 px-4 text-sm font-semibold text-status-failed transition hover:bg-status-failed/20 disabled:cursor-not-allowed disabled:opacity-60"
				>
					<KeyRound size={14} aria-hidden="true" />
					{form.state.isSubmitting ? i18n.common_resetting() : i18n.common_reset()}
				</button>
			</form>
		</div>

		<div
			class="flex flex-col gap-4 p-5 md:flex-row md:items-center md:justify-between md:p-6"
		>
			<div class="flex items-start gap-3.5">
				<span
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-status-failed/12 text-status-failed"
					aria-hidden="true"
				>
					<Unlock size={20} />
				</span>
				<div class="min-w-0 max-w-md">
					<h3 class="text-sm font-semibold text-fg">{i18n.users_clear_lockout()}</h3>
					<p class="mt-1 text-xs text-fg-muted">
						{locked
							? i18n.users_locked_help()
							: i18n.users_no_lockout()}
					</p>
				</div>
			</div>
			<button
				type="button"
				disabled={unlock.isPending}
				onclick={() => unlock.mutate()}
				class="inline-flex h-10 shrink-0 items-center gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 px-4 text-sm font-semibold text-status-failed transition hover:bg-status-failed/20 disabled:cursor-not-allowed disabled:opacity-60"
			>
				<Unlock size={14} aria-hidden="true" />
				{unlock.isPending ? i18n.common_clearing() : i18n.users_clear_lockout()}
			</button>
		</div>

		<div
			class="flex flex-col gap-4 p-5 md:flex-row md:items-center md:justify-between md:p-6"
		>
			<div class="flex items-start gap-3.5">
				<span
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-status-failed/12 text-status-failed"
					aria-hidden="true"
				>
					<Trash2 size={20} />
				</span>
				<div class="min-w-0 max-w-md">
					<h3 class="text-sm font-semibold text-fg">{i18n.users_delete()}</h3>
					<p class="mt-1 text-xs text-fg-muted">
						{isSelf
							? i18n.users_cannot_delete_self()
							: i18n.users_delete_help()}
					</p>
				</div>
			</div>
			<button
				type="button"
				disabled={isSelf || del.isPending}
				onclick={onDelete}
				class="inline-flex h-10 shrink-0 items-center gap-2 rounded-md border border-status-failed bg-status-failed/15 px-4 text-sm font-semibold text-status-failed transition hover:bg-status-failed hover:text-fg-on-accent disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-status-failed/15 disabled:hover:text-status-failed"
			>
				<Trash2 size={14} aria-hidden="true" />
				{del.isPending ? i18n.common_deleting() : i18n.users_delete()}
			</button>
		</div>
	</div>
</section>

<Dialog
	open={confirmDelete}
	title="Delete {user.display_name || user.email}?"
	body="This permanently erases the account and every resource they own."
	onClose={() => (confirmDelete = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{ label: i18n.users_delete(), variant: "danger", onClick: () => del.mutate() },
	]}
/>
