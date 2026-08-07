<script lang="ts">
	import { ChevronRight, LockKeyhole } from "@lucide/svelte";
	import Avatar from "../layout/Avatar.svelte";
	import type { User } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The trailing word is the auth method, not the created date: on a household
	// instance "who signs in through SSO" is the column you scan for, and the
	// date is the one the desktop table sorts by and nobody reads.
	let {
		users,
		selfId,
	}: {
		users: User[];
		selfId: number | undefined;
	} = $props();

	function isLocked(u: User) {
		if (!u.locked_until) return false;
		return new Date(u.locked_until).getTime() > Date.now();
	}

	function rolePill(role: User["role"]) {
		switch (role) {
			case "admin":
				return "bg-status-wanted/10 text-status-wanted";
			case "member":
				return "bg-accent/10 text-accent";
			default:
				return "bg-surface text-fg-muted";
		}
	}
</script>

<ul
	class="-mx-4 divide-y divide-border border-y border-border bg-bg-elevated sm:mx-0 sm:overflow-hidden sm:rounded-lg sm:border lg:hidden"
>
	{#each users as u (u.id)}
		<li>
			<a
				href="/settings/users/{u.id}"
				class="flex min-h-[64px] items-center gap-3 px-4 py-3 transition active:bg-bg-hover"
			>
				<Avatar email={u.email} name={u.display_name} size={34} />
				<div class="min-w-0 flex-1">
					<div class="flex items-center gap-2">
						<span class="min-w-0 truncate text-[15px] font-medium text-fg">
							{u.display_name || u.email}
						</span>
						{#if u.id === selfId}
							<span
								class="shrink-0 rounded-full bg-status-available/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-available"
							>
								{i18n.users_you()}
							</span>
						{/if}
						{#if isLocked(u)}
							<span
								class="inline-flex shrink-0 items-center gap-1 rounded-full bg-status-failed/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
							>
								<LockKeyhole size={10} aria-hidden="true" />
								{i18n.users_locked()}
							</span>
						{/if}
					</div>
					<div class="mt-0.5 truncate font-mono text-[11.5px] text-fg-muted">
						{u.email}
					</div>
				</div>
				<span
					class="shrink-0 rounded-full px-2.5 py-0.5 text-[11px] font-medium {rolePill(
						u.role,
					)}"
				>
					{u.role === "request_only" ? i18n.role_request_only() : u.role}
				</span>
				<span class="shrink-0 font-mono text-[11px] text-fg-faint">
					{u.auth_method}
				</span>
				<ChevronRight
					size={16}
					class="shrink-0 text-fg-faint"
					aria-hidden="true"
				/>
			</a>
		</li>
	{/each}
</ul>
