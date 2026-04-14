<script lang="ts">
	import { Pencil, Trash2, LockKeyhole } from "@lucide/svelte";
	import Avatar from "../layout/Avatar.svelte";
	import type { User } from "../../lib/types";
	import { formatDateTime } from "../../lib/dates";

	let {
		user,
		isSelf,
		onDelete,
	}: {
		user: User;
		isSelf: boolean;
		onDelete: (u: User) => void;
	} = $props();

	let locked = $derived.by(() => {
		if (!user.locked_until) return false;
		return new Date(user.locked_until).getTime() > Date.now();
	});

	let rolePillClass = $derived.by(() => {
		switch (user.role) {
			case "admin":
				return "bg-status-wanted/10 text-status-wanted";
			case "member":
				return "bg-accent/10 text-accent";
			default:
				return "bg-surface text-fg-muted";
		}
	});

	let roleLabel = $derived(
		user.role === "request_only" ? "request only" : user.role,
	);
</script>

<tr class="hover:bg-bg-card">
	<td class="px-4 py-3">
		<a
			href="/settings/users/{user.id}"
			class="flex w-full items-center gap-3 text-left"
		>
			<Avatar email={user.email} name={user.display_name} size={32} />
			<div class="min-w-0">
				<div class="flex items-center gap-2">
					<span class="truncate text-sm font-medium text-fg">
						{user.display_name || user.email}
					</span>
					{#if isSelf}
						<span
							class="inline-flex items-center rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
						>
							you
						</span>
					{/if}
					{#if locked}
						<span
							class="inline-flex items-center gap-1 rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
						>
							<LockKeyhole size={10} aria-hidden="true" />
							locked
						</span>
					{/if}
				</div>
				<div class="truncate font-mono text-xs text-fg-muted">
					{user.email}
				</div>
			</div>
		</a>
	</td>
	<td class="px-4 py-3">
		<span
			class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium {rolePillClass}"
		>
			{roleLabel}
		</span>
	</td>
	<td class="px-4 py-3 text-xs text-fg-muted">{user.auth_method}</td>
	<td class="px-4 py-3 text-xs text-fg-muted">
		{formatDateTime(user.created_at)}
	</td>
	<td class="px-4 py-3 text-right">
		<div class="inline-flex items-center gap-1">
			<a
				href="/settings/users/{user.id}"
				class="inline-flex items-center justify-center rounded-md p-1.5 text-fg-muted hover:bg-surface hover:text-fg"
				aria-label="Edit {user.display_name || user.email}"
			>
				<Pencil size={16} aria-hidden="true" />
			</a>
			{#if !isSelf}
				<button
					type="button"
					onclick={() => onDelete(user)}
					class="inline-flex items-center justify-center rounded-md p-1.5 text-fg-muted hover:bg-status-failed/10 hover:text-status-failed"
					aria-label="Delete {user.display_name || user.email}"
				>
					<Trash2 size={16} aria-hidden="true" />
				</button>
			{/if}
		</div>
	</td>
</tr>
