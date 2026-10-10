<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { BookMarked, Clipboard, Headphones, Plus, RotateCw, ShieldAlert, X } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { formatDateTime, formatRelative } from "@lib/dates";
	import { APP_ACCESS_PATH, appAccessKey, withoutSecret } from "@lib/app-access";
	import type { AppAccess, AppAccessCreated, AppAccessKind } from "@lib/types";
	import Dialog from "@components/modals/Dialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Subsonic and OPDS. Their clients cannot do the app's sign-in, so each user
	// gets one credential per protocol, and both work the same way: a URL to
	// point the client at, the account's email as the username, and a secret
	// made here as the password — a password for music players, a token for
	// e-readers. The secret never goes in a URL. It is shown once, like an API
	// key: the server never answers a read with it, so a lost one is replaced,
	// not recovered, and replacing it signs every client out.
	let { kind }: { kind: AppAccessKind } = $props();

	const META = {
		subsonic: {
			Icon: Headphones,
			title: "Subsonic",
			help: i18n.account_subsonic_help,
			intro: i18n.account_subsonic_intro,
			create: i18n.account_subsonic_create,
			renew: i18n.account_subsonic_renew,
			copyNow: i18n.account_subsonic_copy_now,
			once: i18n.account_subsonic_once,
			renewTitle: i18n.account_subsonic_renew_title,
			renewBody: i18n.account_subsonic_renew_body,
			offTitle: i18n.account_subsonic_off_title,
			offBody: i18n.account_subsonic_off_body,
			created: i18n.account_subsonic_created,
			off: i18n.account_subsonic_off,
		},
		opds: {
			Icon: BookMarked,
			title: "OPDS",
			help: i18n.account_opds_help,
			intro: i18n.account_opds_intro,
			create: i18n.account_opds_create,
			renew: i18n.account_opds_renew,
			copyNow: i18n.account_opds_copy_now,
			once: i18n.account_opds_once,
			renewTitle: i18n.account_opds_renew_title,
			renewBody: i18n.account_opds_renew_body,
			offTitle: i18n.account_opds_off_title,
			offBody: i18n.account_opds_off_body,
			created: i18n.account_opds_created,
			off: i18n.account_opds_off,
		},
	} as const;
	let meta = $derived(META[kind]);
	let path = $derived(APP_ACCESS_PATH[kind]);
	let server = $derived(kind === "subsonic" ? location.origin : `${location.origin}/opds`);

	// Last use is written in batches, up to five minutes behind the client. A
	// credential younger than that with no recorded use may well be in use
	// already, so it reads "not yet" rather than "never", and the card checks
	// again until the window has passed.
	const LAG_MS = 5 * 60_000;
	const awaitingFirstUse = (a: AppAccess | undefined) =>
		!!a?.enabled && !a.last_used_at && !!a.created_at && Date.now() - new Date(a.created_at).getTime() < LAG_MS;

	const qc = useQueryClient();
	const key = () => appAccessKey(kind);
	const q = createQuery<AppAccess>(() => ({
		queryKey: key(),
		queryFn: () => api<AppAccess>(path),
		refetchInterval: (query: { state: { data?: AppAccess } }) => (awaitingFirstUse(query.state.data) ? 30_000 : false),
	}));
	let data = $derived(q.data);

	let revealed = $state<AppAccessCreated | null>(null);
	let confirm = $state<"renew" | "off" | null>(null);

	const create = createMutation<AppAccessCreated, Error, void>(() => ({
		mutationFn: () => api<AppAccessCreated>(path, { method: "POST" }),
		onSuccess: (resp) => {
			revealed = resp;
			qc.setQueryData<AppAccess>(key(), withoutSecret(resp));
			toast.ok(meta.created());
		},
		onError: (err) => toast.err(errorText(err)),
	}));
	const turnOff = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>(path, { method: "DELETE" }),
		onSuccess: () => {
			revealed = null;
			qc.invalidateQueries({ queryKey: key() });
			toast.ok(meta.off());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.ok(i18n.common_copied());
		} catch {
			toast.err(i18n.common_clipboard_unavailable());
		}
	}

	let secret = $derived(revealed?.secret ?? "");

	const btn =
		"inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0";
	const iconBtn =
		"grid h-11 w-11 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg lg:h-8 lg:w-8";
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header class="flex items-start gap-3 border-b border-border px-5 py-3.5">
		<div class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted" aria-hidden="true">
			<meta.Icon size={18} />
		</div>
		<div class="min-w-0 flex-1">
			<h3 class="text-base font-semibold text-fg">{meta.title}</h3>
			<p class="mt-0.5 text-xs text-fg-muted">{meta.help()}</p>
		</div>
	</header>

	{#if revealed}
		<div class="flex flex-col gap-2 border-b border-status-wanted/30 bg-status-wanted/5 px-5 py-4">
			<div class="flex items-start gap-2">
				<ShieldAlert size={16} class="mt-0.5 shrink-0 text-status-wanted" aria-hidden="true" />
				<div class="min-w-0 flex-1">
					<p class="text-sm font-semibold text-fg">{meta.copyNow()}</p>
					<p class="mt-0.5 text-xs text-fg-muted">{meta.once()}</p>
				</div>
				<button type="button" onclick={() => (revealed = null)} class={iconBtn} aria-label={i18n.common_dismiss()}>
					<X size={14} aria-hidden="true" />
				</button>
			</div>
			<code class="block break-all rounded-md bg-bg-deep px-3 py-2 font-mono text-xs text-fg">{secret}</code>
			<button
				type="button"
				onclick={() => copy(secret)}
				class="inline-flex min-h-11 w-fit items-center gap-1.5 rounded-md border border-border bg-bg-base px-2.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg lg:h-8 lg:min-h-0"
			>
				<Clipboard size={12} aria-hidden="true" />
				{i18n.common_copy_clipboard()}
			</button>
		</div>
	{/if}

	{#if q.isPending}
		<SkeletonList variant="divided" count={2} />
	{:else if q.isError}
		<p class="px-5 py-6 text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(q.error) })}
		</p>
	{:else if data?.enabled}
		<dl class="divide-y divide-border">
			{@render row(kind === "subsonic" ? i18n.account_app_server() : i18n.account_app_catalog(), server)}
			{@render row(i18n.account_app_username(), data.username)}
			<div class="flex flex-wrap items-center gap-x-3.5 gap-y-0.5 px-5 py-3 text-xs text-fg-muted">
				<div class="flex items-center gap-1">
					<dt class="text-fg-subtle">{i18n.apikey_created()}</dt>
					<dd title={formatDateTime(data.created_at)}>{formatRelative(data.created_at)}</dd>
				</div>
				<div class="flex min-w-0 items-center gap-1">
					<dt class="text-fg-subtle">{i18n.apikey_last_used()}</dt>
					<dd
						class="min-w-0 truncate"
						title={data.last_used_at ? `${formatDateTime(data.last_used_at)} · ${i18n.account_app_used_lag()}` : i18n.account_app_used_lag()}
					>
						{#if data.last_used_at}
							{formatRelative(data.last_used_at)}{#if data.last_client}<span class="text-fg-subtle">{" · "}</span>{data.last_client}{/if}
						{:else if awaitingFirstUse(data)}
							{i18n.account_app_not_used_yet()}
						{:else}
							{i18n.lc_never()}
						{/if}
					</dd>
				</div>
			</div>
		</dl>
		<div class="flex flex-wrap items-center gap-2 border-t border-border px-5 py-3.5">
			<button
				type="button"
				disabled={create.isPending}
				onclick={() => (confirm = "renew")}
				class="{btn} border border-border bg-bg-card text-fg-muted hover:border-border-strong hover:text-fg"
			>
				<RotateCw size={14} aria-hidden="true" />
				{meta.renew()}
			</button>
			<button
				type="button"
				disabled={turnOff.isPending}
				onclick={() => (confirm = "off")}
				class="{btn} text-status-failed hover:bg-status-failed/10"
			>
				{i18n.account_app_turn_off()}
			</button>
		</div>
	{:else if data}
		<div class="px-5 py-4">
			<p class="text-sm text-fg-muted">{meta.intro()}</p>
			<button
				type="button"
				disabled={create.isPending}
				onclick={() => create.mutate()}
				class="{btn} mt-3 bg-accent font-semibold text-fg-on-accent hover:bg-accent-hover"
			>
				<Plus size={14} aria-hidden="true" />
				{create.isPending ? i18n.common_creating() : meta.create()}
			</button>
		</div>
	{/if}
</section>

{#snippet row(label: string, value: string)}
	<div class="flex items-center gap-3 py-1.5 pl-5 pr-3">
		<dt class="w-24 shrink-0 text-xs text-fg-subtle">{label}</dt>
		<dd class="min-w-0 flex-1 truncate font-mono text-[13px] text-fg" title={value}>{value}</dd>
		<button type="button" onclick={() => copy(value)} class={iconBtn} aria-label={i18n.account_app_copy({ label })}>
			<Clipboard size={14} aria-hidden="true" />
		</button>
	</div>
{/snippet}

<Dialog
	open={confirm !== null}
	title={confirm === "off" ? meta.offTitle() : meta.renewTitle()}
	body={confirm === "off" ? meta.offBody() : meta.renewBody()}
	onClose={() => (confirm = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		confirm === "off"
			? { label: i18n.account_app_turn_off(), variant: "danger", onClick: () => turnOff.mutate() }
			: { label: meta.renew(), variant: "primary", onClick: () => create.mutate() },
	]}
/>
