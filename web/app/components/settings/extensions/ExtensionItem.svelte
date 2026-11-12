<script lang="ts">
	import { ChevronDown, CircleAlert, Pencil, Puzzle, ShieldCheck, Trash2 } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { config, READONLY_HINT } from "@lib/config.svelte";
	import {
		AUTH_LABEL,
		STATE_PILL,
		diffPermissions,
		hasFullAccess,
		permissionSummary,
		shownPermissions,
		type Extension,
	} from "@lib/extensions";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import KebabMenu, { type KebabItem } from "@components/shared/KebabMenu.svelte";
	import ExtensionPermissions from "./ExtensionPermissions.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One extension: who it is, its state, what that state needs from the
	// admin, and what it may do. The state's own content sits under the head —
	// a running one's addresses, the permissions a new version is waiting on,
	// the reason a load failed — and the full permission list folds away under
	// a one-line count, since most visits are about the state, not the grants.
	let {
		ext,
		busy = false,
		onToggle,
		onEdit,
		onApprove,
		onDelete,
	}: {
		ext: Extension;
		busy?: boolean;
		onToggle: (on: boolean) => void;
		onEdit: () => void;
		onApprove: () => void;
		onDelete: () => void;
	} = $props();

	let open = $state(false);
	let pill = $derived(STATE_PILL[ext.state]);
	let perms = $derived(shownPermissions(ext));
	let added = $derived(
		ext.state === "needs_approval" && ext.requested ? diffPermissions(ext.requested, ext.granted).added : null,
	);
	let version = $derived(ext.version ? `v${ext.version}` : "");

	let menu = $derived<KebabItem[]>([
		{ key: "edit", label: i18n.common_edit(), icon: Pencil, onSelect: onEdit },
		...(ext.state === "needs_approval"
			? [{ key: "approve", label: i18n.extensions_approve(), icon: ShieldCheck, onSelect: onApprove }]
			: []),
		{
			key: "delete",
			label: i18n.common_delete(),
			icon: Trash2,
			danger: true,
			dividerBefore: true,
			disabled: config.readOnly,
			title: config.readOnly ? READONLY_HINT : undefined,
			onSelect: onDelete,
		},
	]);
</script>

<article class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<div class="flex items-start gap-3 py-3 pl-4 pr-2 md:py-4 md:pl-5 md:pr-3">
		<div class="mt-0.5 hidden h-10 w-10 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted sm:grid" aria-hidden="true">
			<Puzzle size={19} />
		</div>
		<div class="min-w-0 flex-1 pt-1 sm:pt-0.5">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
				<h3 class="min-w-0 truncate text-[15px] font-semibold text-fg">{ext.name}</h3>
				{#if version}
					<span class="font-mono text-xs text-fg-subtle">{version}</span>
				{:else}
					<span class="text-xs italic text-fg-faint">{i18n.extensions_version_unknown()}</span>
				{/if}
				<LabelPill token={pill.token} label={pill.label()} />
			</div>
			<p class="mt-1 truncate font-mono text-[11.5px] text-fg-subtle" title={ext.source}>{ext.source}</p>
		</div>
		<div class="flex shrink-0 items-center">
			<button
				type="button"
				role="switch"
				aria-checked={ext.enabled}
				aria-label={ext.enabled
					? i18n.extensions_turn_off_name({ name: ext.name })
					: i18n.extensions_turn_on_name({ name: ext.name })}
				title={config.readOnly ? READONLY_HINT : undefined}
				disabled={busy || config.readOnly}
				onclick={() => onToggle(!ext.enabled)}
				class="grid h-11 w-11 place-items-center rounded-md transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
			>
				<span
					class={cn(
						"relative h-[18px] w-[30px] rounded-full border transition",
						ext.enabled ? "border-status-available/50 bg-status-available/20" : "border-border-strong bg-surface-2",
					)}
					aria-hidden="true"
				>
					<span
						class={cn(
							"absolute top-[2px] h-3 w-3 rounded-full transition-all",
							ext.enabled ? "left-[14px] bg-status-available" : "left-[2px] bg-fg-subtle",
						)}
					></span>
				</span>
			</button>
			<KebabMenu items={menu} variant="row" />
		</div>
	</div>

	{#if ext.state === "running" && ext.granted.mounts.length}
		<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t border-border px-4 py-2.5 md:px-5">
			<span class="text-xs text-fg-subtle">{i18n.extensions_serving()}</span>
			{#each ext.granted.mounts as m (m.path)}
				<span class="inline-flex min-w-0 items-baseline gap-1.5">
					<code class="font-mono text-[12.5px] text-fg">{m.path}</code>
					{#if m.auth}<span class="text-xs text-fg-subtle">· {AUTH_LABEL[m.auth]()}</span>{/if}
				</span>
			{/each}
		</div>
	{:else if added}
		<div class="border-t border-status-wanted/25 bg-status-wanted/[0.05] px-4 py-3.5 md:px-5">
			<p class="text-[13px] leading-relaxed text-fg">
				{i18n.extensions_needs_approval_lead({ version: version || ext.name })}
			</p>
			<ExtensionPermissions permissions={added} mark="added" class="mt-2.5" />
			<button
				type="button"
				onclick={onApprove}
				class="mt-3.5 inline-flex min-h-11 items-center gap-1.5 rounded-md bg-accent px-3 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover lg:h-8 lg:min-h-0"
			>
				<ShieldCheck size={14} aria-hidden="true" />
				{i18n.extensions_review_approve()}
			</button>
		</div>
	{:else if ext.state === "failed"}
		<div class="border-t border-status-failed/25 bg-status-failed/[0.05] px-4 py-3 md:px-5">
			<p class="flex items-center gap-1.5 text-[13px] font-medium text-status-failed">
				<CircleAlert size={14} class="shrink-0" aria-hidden="true" />
				{i18n.extensions_failed_lead()}
			</p>
			{#if ext.reason}
				<p class="mt-1 break-words font-mono text-[12px] leading-relaxed text-fg-muted">{ext.reason}</p>
			{/if}
			<button
				type="button"
				onclick={onEdit}
				class="touch-hit mt-1.5 text-xs font-medium text-accent-text hover:underline"
			>
				{i18n.extensions_fix_source()}
			</button>
		</div>
	{/if}

	<div class="border-t border-border">
		<button
			type="button"
			aria-expanded={open}
			onclick={() => (open = !open)}
			class="flex min-h-11 w-full items-center gap-2 px-4 py-2 text-left transition hover:bg-surface md:px-5 lg:min-h-10"
		>
			<span class="shrink-0 text-xs font-medium text-fg-muted">{i18n.extensions_permissions()}</span>
			<span class="min-w-0 flex-1 truncate text-xs text-fg-subtle">
				{#if hasFullAccess(perms)}
					<span class="font-semibold text-status-failed">{i18n.extensions_full_access_title()}</span>{permissionSummary(perms) ? " · " : ""}
				{/if}
				{permissionSummary(perms)}
			</span>
			<ChevronDown size={14} class={cn("shrink-0 text-fg-faint transition-transform", open && "rotate-180")} aria-hidden="true" />
		</button>
		{#if open}
			<div class="px-4 pb-4 pt-1 md:px-5">
				<ExtensionPermissions permissions={perms} />
			</div>
		{/if}
	</div>
</article>
