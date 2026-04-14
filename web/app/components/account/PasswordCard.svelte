<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { password } from "../../lib/schemas";
	import TextField from "../forms/TextField.svelte";
	import Modal from "../modals/Modal.svelte";

	type Body = { current_password: string; new_password: string };

	let open = $state(false);

	const mutation = createMutation<null, Error, Body>(() => ({
		mutationFn: (body) =>
			api<null>("/auth/password", { method: "POST", body }),
		onSuccess: () => {
			form.reset();
			toast.ok("Password changed");
			open = false;
		},
		onError: (err) => toast.err(err.message),
	}));

	const form = createForm(() => ({
		defaultValues: {
			current_password: "",
			new_password: "",
			confirm_password: "",
		},
		validators: {
			onChange: v.object({
				current_password: v.pipe(v.string(), v.minLength(1, "Required")),
				new_password: password,
				confirm_password: v.string(),
			}),
		},
		onSubmit: ({ value }) => {
			if (value.new_password !== value.confirm_password) {
				toast.err("Passwords don't match");
				return;
			}
			mutation.mutate({
				current_password: value.current_password,
				new_password: value.new_password,
			});
		},
	}));

	// 0–4 score from length + character-class diversity. Tracks the visual
	// segments rather than pretending to grade entropy.
	function strengthOf(pw: string): 0 | 1 | 2 | 3 | 4 {
		if (!pw) return 0;
		let classes = 0;
		if (/[a-z]/.test(pw)) classes++;
		if (/[A-Z]/.test(pw)) classes++;
		if (/[0-9]/.test(pw)) classes++;
		if (/[^A-Za-z0-9]/.test(pw)) classes++;
		const len = pw.length;
		if (len < 8) return 1;
		if (len < 12 && classes <= 2) return 2;
		if (len < 14 || classes <= 2) return 3;
		return 4;
	}

	const LABELS = ["—", "WEAK", "FAIR", "GOOD", "STRONG"] as const;
	const FILL_CLASSES = [
		"bg-bg-deep",
		"bg-status-failed",
		"bg-status-wanted",
		"bg-status-wanted",
		"bg-status-available",
	] as const;
	const PILL_CLASSES = [
		"bg-surface text-fg-faint",
		"bg-status-failed/15 text-status-failed",
		"bg-status-wanted/15 text-status-wanted",
		"bg-status-wanted/15 text-status-wanted",
		"bg-status-available/15 text-status-available",
	] as const;

	function startChange() {
		form.reset();
		open = true;
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-6">
	<header class="mb-3 flex items-start justify-between gap-3">
		<h3 class="text-base font-semibold text-fg">Password</h3>
		<!-- We never see the cleartext, so this presents the static enforced
		     minimum rather than pretending to grade the current secret. -->
		<span
			class="inline-flex items-center rounded-full bg-status-available/15 px-2 py-0.5 font-mono text-[9.5px] font-semibold uppercase tracking-[0.1em] text-status-available"
		>
			Set
		</span>
	</header>

	<p class="text-xs text-fg-muted">
		Sign in with email and password. Other sessions sign out after a
		change.
	</p>

	<!-- Decorative meter matching the prototype's "this password slot is
	     active" affordance; the real interactive meter lives in the modal. -->
	<div class="mt-4 flex gap-1" aria-hidden="true">
		{#each [0, 1, 2, 3] as i (i)}
			<span class="h-1 flex-1 rounded-full bg-status-available/60"></span>
		{/each}
	</div>

	<button
		type="button"
		onclick={startChange}
		class="mt-4 inline-flex h-9 items-center rounded-md border border-border bg-surface px-3.5 text-sm font-medium text-fg transition hover:bg-surface-2"
	>
		Change password
	</button>
</section>

<Modal
	{open}
	title="Change password"
	size="md"
	onClose={() => {
		if (!mutation.isPending) open = false;
	}}
>
	<form
		id="change-password-form"
		class="grid gap-3"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<form.Field name="current_password">
			{#snippet children(field)}
				<TextField
					{field}
					label="Current password"
					type="password"
					autocomplete="current-password"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="new_password">
			{#snippet children(field)}
				{@const score = strengthOf(field.state.value ?? "")}
				<div class="grid gap-1.5">
					<TextField
						{field}
						label="New password"
						type="password"
						autocomplete="new-password"
						help="At least 8 characters. Mix letters, numbers, symbols for a stronger score."
					/>
					<div class="flex items-center gap-2">
						<div
							class="flex flex-1 gap-1"
							role="meter"
							aria-label="Password strength"
							aria-valuemin={0}
							aria-valuemax={4}
							aria-valuenow={score}
						>
							{#each [1, 2, 3, 4] as i (i)}
								<span
									class={[
										"h-1 flex-1 rounded-full transition-colors",
										score >= i ? FILL_CLASSES[score] : "bg-bg-deep",
									]}
								></span>
							{/each}
						</div>
						<span
							class={[
								"inline-flex items-center rounded-full px-2 py-0.5 font-mono text-[9.5px] font-semibold uppercase tracking-[0.1em]",
								PILL_CLASSES[score],
							]}
						>
							{LABELS[score]}
						</span>
					</div>
				</div>
			{/snippet}
		</form.Field>
		<form.Field name="confirm_password">
			{#snippet children(field)}
				{@const newPw = form.state.values.new_password}
				{@const mismatched =
					!!field.state.value &&
					!!newPw &&
					field.state.value !== newPw}
				<div class="grid gap-1">
					<TextField
						{field}
						label="Confirm new password"
						type="password"
						autocomplete="new-password"
					/>
					{#if mismatched}
						<p
							class="text-xs text-status-failed"
							role="alert"
							aria-live="polite"
						>
							Passwords don't match.
						</p>
					{/if}
				</div>
			{/snippet}
		</form.Field>
	</form>

	{#snippet footer()}
		<button
			type="button"
			onclick={() => {
				if (!mutation.isPending) open = false;
			}}
			disabled={mutation.isPending}
			class="inline-flex h-9 items-center rounded-md border border-border bg-surface px-3.5 text-sm font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg disabled:cursor-not-allowed disabled:opacity-60"
		>
			Cancel
		</button>
		<button
			type="submit"
			form="change-password-form"
			disabled={mutation.isPending}
			class="inline-flex h-9 items-center rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{mutation.isPending ? "Changing…" : "Change password"}
		</button>
	{/snippet}
</Modal>
