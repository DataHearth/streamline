<script lang="ts">
	import { ArrowDown, ArrowUp, ArrowUpDown } from "@lucide/svelte";
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import { Users, Search, UserPlus, SlidersHorizontal } from "@lucide/svelte";
	import { api, errorText } from "../../../lib/api";
	import { auth } from "../../../lib/auth.svelte";
	import { toast } from "../../../lib/toast";
	import { requireAdmin } from "../../../lib/guards";
	import { cn } from "../../../lib/cn";
	import { email, password, displayName, userRole } from "../../../lib/schemas";
	import type { User, UserList, UserRole } from "../../../lib/types";
	import UserRow from "../../../components/users/UserRow.svelte";
	import UserTouchList from "../../../components/users/UserTouchList.svelte";
	import UserFilterSheet from "../../../components/users/UserFilterSheet.svelte";
	import InvitesCard from "../../../components/users/InvitesCard.svelte";
	import TextField from "../../../components/forms/TextField.svelte";
	import Select from "../../../components/forms/Select.svelte";
	import Dialog from "../../../components/modals/Dialog.svelte";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	const LIMIT = 25;

	type SortKey = "name" | "role" | "auth" | "created";
	type SortDir = "asc" | "desc";

	let q = $state("");
	let debouncedQ = $state("");
	let role = $state<UserRole | "">("");
	let offset = $state(0);
	let sort = $state<SortKey>("created");
	let order = $state<SortDir>("desc");
	// Touch: the sort control was the table header, so it moves into a sheet
	// alongside the role filter. See UserFilterSheet.
	let sheetOpen = $state(false);

	const qc = useQueryClient();

	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	$effect(() => {
		// reset to first page on filter or sort change
		void role;
		void sort;
		void order;
		offset = 0;
	});

	// q feeds the query key, so every keystroke would otherwise be its own cache
	// entry — a fresh request plus a drop back to the loading state per letter.
	// The page reset rides along inside the timer so the search term and offset
	// land in the same update, leaving the query key one transition to make.
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const raw = q;
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => {
			const next = raw.trim();
			if (next === debouncedQ) return;
			debouncedQ = next;
			offset = 0;
		}, 300);
		return () => clearTimeout(debounceTimer);
	});

	const users = createQuery<UserList>(() => ({
		queryKey: [
			"users",
			{ q: debouncedQ, role, sort, order, offset, limit: LIMIT },
		],
		queryFn: () => {
			const params = new URLSearchParams({
				limit: String(LIMIT),
				offset: String(offset),
				sort,
				order,
			});
			if (debouncedQ) params.set("q", debouncedQ);
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
		onError: (err) => toast.err(errorText(err)),
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
		onError: (err) => toast.err(errorText(err)),
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
	let hasFilter = $derived(debouncedQ.length > 0 || role !== "");
	let from = $derived(items.length ? offset + 1 : 0);
	let to = $derived(offset + items.length);
	let hasPrev = $derived(offset > 0);
	let hasNext = $derived(offset + LIMIT < total);

	// Badge count on the touch filter button: a role filter, and a sort that is
	// no longer the default. Search has its own visible field and never counts.
	let activeFilters = $derived(
		(role !== "" ? 1 : 0) + (sort !== "created" || order !== "desc" ? 1 : 0),
	);
	function resetFilters() {
		role = "";
		sort = "created";
		order = "desc";
	}
</script>

<header class="flex flex-wrap items-end justify-between gap-3">
	<div class="flex items-start gap-3">
		<span
			class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-accent/10 text-accent"
		>
			<Users size={18} aria-hidden="true" />
		</span>
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_users()}</h1>
			<p class="mt-0.5 text-sm text-fg-muted">
				{total} total — admins, members, and request-only accounts.
			</p>
		</div>
	</div>
	<button
		type="button"
		onclick={() => (creating = true)}
		class="inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3.5 py-2.5 text-sm font-medium text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2 sm:w-auto sm:py-2"
	>
		<UserPlus size={16} aria-hidden="true" />
		{i18n.users_new()}
	</button>
</header>

<section
	class="mt-6 lg:rounded-lg lg:border lg:border-border lg:bg-bg-elevated lg:p-6"
>
	<form
		class="grid gap-3 lg:grid-cols-[1fr_220px_auto] lg:items-end"
		onsubmit={(e) => e.preventDefault()}
	>
		<div class="flex items-end gap-2.5">
			<label class="block min-w-0 flex-1">
				<span class="mb-1 block text-xs font-medium text-fg-muted"
					>{i18n.common_search()}</span
				>
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
						placeholder={i18n.field_email_or_name()}
						autocomplete="off"
						class="w-full rounded-md bg-transparent py-2 pl-9 pr-3 text-sm text-fg placeholder:text-fg-faint focus:outline-none"
					/>
				</span>
			</label>
			<button
				type="button"
				onclick={() => (sheetOpen = true)}
				aria-label={i18n.users_filter_sort()}
				class="relative grid h-11 w-11 shrink-0 place-items-center rounded-md border border-border text-fg-muted transition active:bg-bg-hover lg:hidden"
			>
				<SlidersHorizontal size={17} aria-hidden="true" />
				{#if activeFilters > 0}
					<span
						class="absolute -right-1.5 -top-1.5 grid h-[17px] min-w-[17px] place-items-center rounded-full bg-accent px-1 font-mono text-[9.5px] font-bold text-fg-on-accent"
					>
						{activeFilters}
					</span>
				{/if}
			</button>
		</div>
		<div class="hidden lg:block">
			<span class="mb-1 block text-xs font-medium text-fg-muted"
				>{i18n.common_role()}</span
			>
			<Select
				ariaLabel="Filter by role"
				value={role}
				options={[
					{ value: "", label: i18n.role_all() },
					{ value: "admin", label: i18n.common_admin() },
					{ value: "member", label: i18n.role_member() },
					{ value: "request_only", label: i18n.role_request_only() },
				]}
				onChange={(v) => (role = v)}
			/>
		</div>
	</form>

	<div class="mt-5">
		{#if users.isPending}
			<p class="px-1 py-4 text-sm text-fg-subtle">{i18n.common_loading()}</p>
		{:else if users.isError}
			<p class="px-1 py-4 text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(users.error) })}
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
					{hasFilter ? i18n.users_no_match() : i18n.users_none_yet()}
				</p>
				<p class="mt-1 text-xs text-fg-muted">
					{hasFilter
						? i18n.users_no_match_help()
						: i18n.users_none_help()}
				</p>
			</div>
		{:else}
			<UserTouchList users={items} selfId={auth.user?.id} />

			<!-- min-w-[560px] in a horizontal scroller inside a vertically scrolling
			     page: available from lg, where the column is finally wide enough. -->
			<div class="hidden overflow-x-auto rounded-md border border-border lg:block">
				<table class="w-full min-w-[560px] text-sm">
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
					class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 hover:border-accent disabled:cursor-not-allowed disabled:opacity-40"
				>
					{i18n.common_prev()}
				</button>
				<button
					type="button"
					disabled={!hasNext}
					onclick={() => (offset += LIMIT)}
					class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center rounded-md border border-border px-3 hover:border-accent disabled:cursor-not-allowed disabled:opacity-40"
				>
					{i18n.common_next()}
				</button>
			</div>
		</div>
	</div>
</section>

<section class="mt-6">
	<InvitesCard />
</section>

<UserFilterSheet
	open={sheetOpen}
	onClose={() => (sheetOpen = false)}
	{role}
	onRoleChange={(r) => (role = r)}
	{sort}
	{order}
	onSortChange={toggleSort}
	resultCount={total}
	activeCount={activeFilters}
	onReset={resetFilters}
/>

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
	title={i18n.users_new()}
	onClose={closeCreate}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost" },
		{
			label: i18n.users_create(),
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
					label={i18n.common_email()}
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
					label={i18n.common_password()}
					type="password"
					autocomplete="new-password"
					help={i18n.validation_password_min()}
				/>
			{/snippet}
		</form.Field>
		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.field_display_name()}
					autocomplete="off"
					placeholder="Jane Doe"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="role">
			{#snippet children(field)}
				<Select
					label={i18n.common_role()}
					value={field.state.value as UserRole}
					onChange={(v) => field.handleChange(v)}
					options={[
						{ value: "member", label: i18n.role_member() },
						{ value: "request_only", label: i18n.role_request_only() },
						{ value: "admin", label: i18n.common_admin() },
					]}
				/>
			{/snippet}
		</form.Field>
		<button type="submit" class="sr-only" tabindex="-1" aria-hidden="true">
			{i18n.common_create()}
		</button>
	</form>
</Dialog>

<Dialog
	open={deleting !== null}
	title="Delete {deleting?.display_name || deleting?.email || ''}?"
	body="This permanently erases every resource they own."
	onClose={() => (deleting = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.users_delete(),
			variant: "danger",
			onClick: () => deleting && deleteUser.mutate(deleting.id),
		},
	]}
/>
