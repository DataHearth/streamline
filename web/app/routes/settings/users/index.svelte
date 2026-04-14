<script lang="ts">
	import { ArrowDown, ArrowUp, ArrowUpDown } from "@lucide/svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import { Users, Search, UserPlus } from "@lucide/svelte";
	import { api } from "../../../lib/api";
	import { auth } from "../../../lib/auth.svelte";
	import { toast } from "../../../lib/toast";
	import { requireAdmin } from "../../../lib/guards";
	import { cn } from "../../../lib/cn";
	import { email, password, displayName, userRole } from "../../../lib/schemas";
	import type { User, UserList, UserRole } from "../../../lib/types";
	import UserRow from "../../../components/users/UserRow.svelte";
	import InvitesCard from "../../../components/users/InvitesCard.svelte";
	import TextField from "../../../components/forms/TextField.svelte";
	import Select from "../../../components/forms/Select.svelte";
	import Dialog from "../../../components/modals/Dialog.svelte";

	const LIMIT = 25;

	type SortKey = "name" | "role" | "auth" | "created";
	type SortDir = "asc" | "desc";

	let q = $state("");
	let role = $state<UserRole | "">("");
	let offset = $state(0);
	let sort = $state<SortKey>("created");
	let order = $state<SortDir>("desc");

	const qc = useQueryClient();

	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	$effect(() => {
		// reset to first page on filter or sort change
		void q;
		void role;
		void sort;
		void order;
		offset = 0;
	});

	const users = createQuery<UserList>(() => ({
		queryKey: ["users", { q, role, sort, order, offset, limit: LIMIT }],
		queryFn: () => {
			const params = new URLSearchParams({
				limit: String(LIMIT),
				offset: String(offset),
				sort,
				order,
			});
			if (q.trim()) params.set("q", q.trim());
			if (role) params.set("role", role);
			return api<UserList>(`/users?${params.toString()}`);
		},
		enabled: !auth.loading && auth.user?.role === "admin",
	}));

	function toggleSort(key: SortKey) {
		if (sort === key) {
			order = order === "asc" ? "desc" : "asc";
		} else {
			sort = key;
			order = key === "created" ? "desc" : "asc";
		}
	}

	const deleteUser = createMutation<null, Error, number>(() => ({
		mutationFn: (id) => api<null>(`/users/${id}`, { method: "DELETE" }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["users"] });
			toast.ok("User deleted");
		},
		onError: (err) => toast.err(err.message),
	}));

	let deleting = $state<User | null>(null);
	function onDelete(u: User) {
		deleting = u;
	}

	let creating = $state(false);

	type CreateUserBody = {
		email: string;
		password: string;
		role: UserRole;
		display_name?: string;
	};

	const create = createMutation<User, Error, CreateUserBody>(() => ({
		mutationFn: (body) => api<User>("/users", { method: "POST", body }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["users"] });
			toast.ok("User created");
			closeCreate();
		},
		onError: (err) => toast.err(err.message),
	}));

	const form = createForm(() => ({
		defaultValues: {
			email: "",
			password: "",
			display_name: "",
			role: "member" as UserRole,
		},
		validators: {
			onChange: v.object({
				email,
				password,
				display_name: displayName,
				role: userRole,
			}),
		},
		onSubmit: ({ value }) =>
			create.mutate({
				email: value.email,
				password: value.password,
				role: value.role,
				...(value.display_name.trim()
					? { display_name: value.display_name.trim() }
					: {}),
			}),
	}));

	function closeCreate() {
		creating = false;
		form.reset();
	}

	let items = $derived(users.data?.items ?? []);
	let total = $derived(users.data?.total ?? 0);
	let hasFilter = $derived(q.trim().length > 0 || role !== "");
	let from = $derived(items.length ? offset + 1 : 0);
	let to = $derived(offset + items.length);
	let hasPrev = $derived(offset > 0);
	let hasNext = $derived(offset + LIMIT < total);
</script>

<header class="flex flex-wrap items-end justify-between gap-3">
	<div class="flex items-start gap-3">
		<span
			class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-accent/10 text-accent"
		>
			<Users size={18} aria-hidden="true" />
		</span>
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">Users</h1>
			<p class="mt-0.5 text-sm text-fg-muted">
				{total} total — admins, members, and request-only accounts.
			</p>
		</div>
	</div>
	<button
		type="button"
		onclick={() => (creating = true)}
		class="inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-medium text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
	>
		<UserPlus size={16} aria-hidden="true" />
		New user
	</button>
</header>

<section
	class="mt-6 rounded-lg border border-border bg-bg-elevated p-5 md:p-6"
>
	<form
		class="grid gap-3 sm:grid-cols-[1fr_220px_auto] sm:items-end"
		onsubmit={(e) => e.preventDefault()}
	>
		<label class="block">
			<span class="mb-1 block text-xs font-medium text-fg-muted">Search</span>
			<span
				class="relative flex items-center rounded-md border border-border bg-bg focus-within:border-accent"
			>
				<Search
					size={16}
					class="absolute left-3 text-fg-faint"
					aria-hidden="true"
				/>
				<input
					type="search"
					bind:value={q}
					placeholder="Email or name"
					autocomplete="off"
					class="w-full rounded-md bg-transparent py-2 pl-9 pr-3 text-sm text-fg placeholder:text-fg-faint focus:outline-none"
				/>
			</span>
		</label>
		<div class="block">
			<span class="mb-1 block text-xs font-medium text-fg-muted">Role</span>
			<Select
				ariaLabel="Filter by role"
				value={role}
				options={[
					{ value: "", label: "All roles" },
					{ value: "admin", label: "Admin" },
					{ value: "member", label: "Member" },
					{ value: "request_only", label: "Request only" },
				]}
				onChange={(v) => (role = v)}
			/>
		</div>
	</form>

	<div class="mt-5">
		{#if users.isPending}
			<p class="px-1 py-4 text-sm text-fg-subtle">Loading…</p>
		{:else if users.isError}
			<p class="px-1 py-4 text-sm text-status-failed">
				Failed to load users: {users.error?.message}
			</p>
		{:else if items.length === 0}
			<div
				class="rounded-md border border-dashed border-border bg-bg-deep/40 px-6 py-10 text-center"
			>
				<Users
					size={24}
					class="mx-auto text-fg-faint"
					aria-hidden="true"
				/>
				<p class="mt-3 text-sm text-fg">
					{hasFilter ? "No users match this filter." : "No users yet."}
				</p>
				<p class="mt-1 text-xs text-fg-muted">
					{hasFilter
						? "Try widening the search or clearing the role filter."
						: "Create an invite below to onboard a teammate."}
				</p>
			</div>
		{:else}
			<div class="overflow-hidden rounded-md border border-border">
				<table class="w-full text-sm">
					<thead
						class="bg-surface text-left text-xs uppercase tracking-wider text-fg-muted"
					>
						<tr>
							{@render sortHeader("name", "User")}
							{@render sortHeader("role", "Role")}
							{@render sortHeader("auth", "Auth")}
							{@render sortHeader("created", "Created")}
							<th class="px-4 py-2.5"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-border">
						{#each items as u (u.id)}
							<UserRow
								user={u}
								isSelf={u.id === auth.user?.id}
								{onDelete}
							/>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<div
			class="mt-4 flex h-9 items-center justify-between text-sm text-fg-muted"
		>
			<span>
				{items.length ? `${from}–${to} of ${total}` : `0 of ${total}`}
			</span>
			<div class="flex gap-2">
				<button
					type="button"
					disabled={!hasPrev}
					onclick={() => (offset = Math.max(0, offset - LIMIT))}
					class="inline-flex h-9 items-center rounded-md border border-border px-3 hover:border-accent disabled:cursor-not-allowed disabled:opacity-40"
				>
					Prev
				</button>
				<button
					type="button"
					disabled={!hasNext}
					onclick={() => (offset += LIMIT)}
					class="inline-flex h-9 items-center rounded-md border border-border px-3 hover:border-accent disabled:cursor-not-allowed disabled:opacity-40"
				>
					Next
				</button>
			</div>
		</div>
	</div>
</section>

<section class="mt-6">
	<InvitesCard />
</section>

{#snippet sortHeader(key: SortKey, label: string)}
	{@const active = sort === key}
	<th
		class="px-4 py-2.5"
		aria-sort={active ? (order === "asc" ? "ascending" : "descending") : "none"}
	>
		<button
			type="button"
			onclick={() => toggleSort(key)}
			class={cn(
				"inline-flex items-center gap-1.5 font-semibold uppercase tracking-wider transition-colors",
				active ? "text-fg" : "hover:text-fg",
			)}
		>
			{label}
			{#if active}
				{#if order === "asc"}
					<ArrowUp size={12} class="text-accent" aria-hidden="true" />
				{:else}
					<ArrowDown size={12} class="text-accent" aria-hidden="true" />
				{/if}
			{:else}
				<ArrowUpDown size={12} class="text-fg-faint" aria-hidden="true" />
			{/if}
		</button>
	</th>
{/snippet}

<Dialog
	open={creating}
	title="New user"
	onClose={closeCreate}
	actions={[
		{ label: "Cancel", variant: "ghost" },
		{
			label: "Create user",
			variant: "primary",
			onClick: () => form.handleSubmit(),
			dismiss: false,
			pending: create.isPending,
		},
	]}
>
	<form
		class="grid gap-3"
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
		<form.Field name="password">
			{#snippet children(field)}
				<TextField
					{field}
					label="Password"
					type="password"
					autocomplete="new-password"
					help="At least 8 characters"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label="Display name (optional)"
					autocomplete="off"
					placeholder="Jane Doe"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="role">
			{#snippet children(field)}
				<Select
					label="Role"
					value={field.state.value as UserRole}
					onChange={(v) => field.handleChange(v)}
					options={[
						{ value: "member", label: "Member" },
						{ value: "request_only", label: "Request only" },
						{ value: "admin", label: "Admin" },
					]}
				/>
			{/snippet}
		</form.Field>
		<button type="submit" class="sr-only" tabindex="-1" aria-hidden="true">
			Create
		</button>
	</form>
</Dialog>

<Dialog
	open={deleting !== null}
	title="Delete {deleting?.display_name || deleting?.email || ''}?"
	body="This permanently erases every resource they own."
	onClose={() => (deleting = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Delete user",
			variant: "danger",
			onClick: () => deleting && deleteUser.mutate(deleting.id),
		},
	]}
/>
