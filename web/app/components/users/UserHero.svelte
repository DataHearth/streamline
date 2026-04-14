<script lang="ts">
	import { Shield, KeyRound, Monitor, LockKeyhole, ShieldCheck } from "@lucide/svelte";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import type { ApiKey, Session, User } from "../../lib/types";
	import Avatar from "../layout/Avatar.svelte";

	let {
		user,
		apiKeys,
		sessions,
	}: { user: User; apiKeys: ApiKey[]; sessions: Session[] } = $props();

	let locked = $derived.by(() => {
		if (!user.locked_until) return false;
		return new Date(user.locked_until).getTime() > Date.now();
	});

	let primary = $derived(user.display_name?.trim() || user.email);

	let lastSeen = $derived.by(() => {
		const ts = sessions
			.map((s) => s.last_seen_at ?? s.created_at)
			.filter((v): v is string => Boolean(v))
			.sort((a, b) => (a < b ? 1 : -1));
		return ts[0] ?? null;
	});

	let roleLabel = $derived(
		user.role === "request_only" ? "request only" : user.role,
	);
</script>

<section
	class="relative overflow-hidden rounded-xl border border-border bg-bg-elevated"
>
	<div
		class="pointer-events-none absolute inset-0 bg-gradient-to-br from-accent/8 via-transparent to-transparent"
		aria-hidden="true"
	></div>

	<div
		class="relative flex flex-col gap-6 p-6 md:flex-row md:items-center md:justify-between md:p-7"
	>
		<div class="flex items-center gap-4">
			<Avatar email={user.email} name={user.display_name} size={56} />
			<div class="min-w-0">
				<div class="flex flex-wrap items-center gap-2">
					<h2
						class="truncate text-xl font-bold tracking-tight text-fg md:text-2xl"
					>
						{primary}
					</h2>
					{#if user.role === "admin"}
						<span
							class="inline-flex items-center gap-1 rounded-full bg-accent/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent"
						>
							<ShieldCheck size={10} aria-hidden="true" />
							Admin
						</span>
					{:else}
						<span
							class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
						>
							{roleLabel}
						</span>
					{/if}
					{#if locked}
						<span
							class="inline-flex items-center gap-1 rounded-full bg-status-failed/12 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-failed"
						>
							<LockKeyhole size={10} aria-hidden="true" />
							locked
						</span>
					{/if}
				</div>
				<p class="mt-1 truncate font-mono text-sm text-fg-muted">
					{user.email}
				</p>
				<p class="mt-1 text-xs text-fg-subtle">
					Member since {formatDateTime(user.created_at)}
					{#if user.failed_login_count && user.failed_login_count > 0}
						· {user.failed_login_count} failed sign-in{user.failed_login_count === 1 ? "" : "s"}
					{/if}
				</p>
			</div>
		</div>

		<dl
			class="grid grid-cols-3 gap-px overflow-hidden rounded-lg border border-border bg-border md:max-w-md md:flex-1"
		>
			{@render stat(Monitor, "Sessions", String(sessions.length))}
			{@render stat(KeyRound, "API keys", String(apiKeys.length))}
			{@render stat(
				Shield,
				"Last seen",
				lastSeen ? formatRelative(lastSeen) : "—",
			)}
		</dl>
	</div>
</section>

{#snippet stat(Icon: typeof Shield, label: string, value: string)}
	<div class="flex flex-col gap-1 bg-bg-elevated px-3.5 py-3">
		<dt
			class="flex items-center gap-1.5 text-[10.5px] font-semibold uppercase tracking-[0.12em] text-fg-faint"
		>
			<Icon size={11} aria-hidden="true" />
			{label}
		</dt>
		<dd class="truncate text-sm font-semibold text-fg">{value}</dd>
	</div>
{/snippet}
