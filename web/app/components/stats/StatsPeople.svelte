<script lang="ts">
	import Avatar from "@components/layout/Avatar.svelte";
	import { formatRelative } from "@lib/dates";
	import { roleLabel } from "@lib/roles";
	import { fmtCount, type StatsPerson } from "@lib/stats";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Who asks for what, and how it went: requests in the range by outcome,
	// with the last time each person signed in. Accounts that asked for
	// nothing stay in, at the bottom: a member who never requests is a fact
	// worth seeing too. A row opens the user in Settings → Users.
	let { people }: { people: StatsPerson[] } = $props();

	const head = "font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint";
	const num = (n: number, cls: string) => (n > 0 ? cls : "text-fg-faint");
</script>

<div class="min-w-0">
	<div class="ppl-grid hidden border-b border-border px-4 pb-2 md:grid md:px-5" aria-hidden="true">
		<span class={head}>{i18n.common_person()}</span>
		<span class={cn(head, "text-right")}>{i18n.requests_label()}</span>
		<span class={cn(head, "text-right")}>{i18n.common_approved()}</span>
		<span class={cn(head, "text-right")}>{i18n.stats_denied()}</span>
		<span class={cn(head, "text-right")}>{i18n.status_available()}</span>
		<span class={cn(head, "text-right")}>{i18n.stats_col_last_seen()}</span>
	</div>
	<ul>
		{#each people as p, i (p.id)}
			<li class={i > 0 ? "border-t border-border" : ""}>
				<a
					href="/settings/users/{p.id}"
					class="block px-4 py-2.5 transition hover:bg-surface focus:outline-none focus-visible:bg-surface md:px-5"
				>
					<!-- Phone -->
					<div class="flex items-center gap-3 md:hidden">
						<Avatar email={p.email} name={p.name} size={32} />
						<div class="min-w-0 flex-1">
							<div class="flex items-baseline gap-2">
								<span class="min-w-0 flex-1 truncate text-[13.5px] font-medium text-fg">{p.name}</span>
								<span class="font-mono text-[12px] tabular-nums text-fg">{fmtCount(p.requests)}</span>
							</div>
							<div class="mt-0.5 flex items-baseline gap-2.5 font-mono text-[11px] text-fg-subtle">
								<span class="min-w-0 flex-1 truncate">
									{#if p.available > 0}<span class="text-status-available">{p.available}</span> {i18n.lc_available()} · {/if}{#if p.denied > 0}<span class="text-status-failed">{p.denied}</span> {i18n.lc_denied()} · {/if}{roleLabel(p.role)}
								</span>
								<span class="shrink-0">{p.last_seen_at ? formatRelative(p.last_seen_at) : i18n.common_never()}</span>
							</div>
						</div>
					</div>
					<!-- md and up -->
					<div class="ppl-grid hidden items-center md:grid">
						<span class="flex min-w-0 items-center gap-2.5">
							<Avatar email={p.email} name={p.name} size={28} />
							<span class="min-w-0">
								<span class="block truncate text-[13px] font-medium text-fg">{p.name}</span>
								<span class="block truncate font-mono text-[10.5px] text-fg-subtle">{roleLabel(p.role)}</span>
							</span>
						</span>
						<span class={cn("text-right font-mono text-[12.5px] tabular-nums", num(p.requests, "text-fg"))}>{fmtCount(p.requests)}</span>
						<span class={cn("text-right font-mono text-[12.5px] tabular-nums", num(p.approved, "text-status-grabbing"))}>{fmtCount(p.approved)}</span>
						<span class={cn("text-right font-mono text-[12.5px] tabular-nums", num(p.denied, "text-status-failed"))}>{fmtCount(p.denied)}</span>
						<span class={cn("text-right font-mono text-[12.5px] tabular-nums", num(p.available, "text-status-available"))}>{fmtCount(p.available)}</span>
						<span class="truncate text-right font-mono text-[11.5px] text-fg-subtle">
							{p.last_seen_at ? formatRelative(p.last_seen_at) : i18n.common_never()}
						</span>
					</div>
				</a>
			</li>
		{/each}
	</ul>
</div>

<style>
	.ppl-grid {
		grid-template-columns: minmax(0, 1.6fr) 64px 64px 56px 64px minmax(0, 1fr);
		column-gap: 12px;
	}
</style>
