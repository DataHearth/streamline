<script lang="ts">
	import {
		ChevronRight,
		User,
		LockKeyhole,
		Globe,
		KeyRound,
		Monitor,
		RefreshCw,
	} from "@lucide/svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import type { ApiKey, Session } from "../../lib/types";
	import { getLocale, locales } from "../../lib/paraglide/runtime.js";
	import FullScreenPanel from "../layout/FullScreenPanel.svelte";
	import ProfileCard from "./ProfileCard.svelte";
	import PasswordCard from "./PasswordCard.svelte";
	import LanguageCard from "./LanguageCard.svelte";
	import APIKeysCard from "./APIKeysCard.svelte";
	import SessionsCard from "./SessionsCard.svelte";
	import JWTRotateCard from "./JWTRotateCard.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Touch account: the same four sections, but as rows that state their
	// current value — Antoine Langlois, Français, 2 keys, 3 sessions — instead
	// of cards that put the value below a heading you have to scroll past. Each
	// row opens its card as a screen of its own.
	//
	// The cards themselves are unchanged; only where they are rendered moves.
	let isAdmin = $derived(auth.user?.role === "admin");

	// Same query keys as the cards and IdentityHero, so this shares their cache
	// rather than issuing a second round of requests.
	const keys = createQuery<ApiKey[]>(() => ({
		queryKey: ["auth", "me", "api-keys"],
		queryFn: () => api<ApiKey[]>("/auth/me/api-keys"),
	}));
	const sessions = createQuery<Session[]>(() => ({
		queryKey: ["auth", "me", "sessions"],
		queryFn: () => api<Session[]>("/auth/me/sessions"),
	}));

	type Locale = (typeof locales)[number];
	function endonym(code: Locale): string {
		const name =
			new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;
		return name.charAt(0).toLocaleUpperCase(code) + name.slice(1);
	}

	type RowKey =
		| "profile"
		| "password"
		| "language"
		| "keys"
		| "sessions"
		| "jwt";

	let openRow = $state<RowKey | null>(null);

	let titles = $derived<Record<RowKey, string>>({
		profile: i18n.common_profile(),
		password: i18n.common_password(),
		language: i18n.account_language(),
		keys: i18n.account_api_keys(),
		sessions: i18n.account_active_sessions(),
		jwt: i18n.account_rotate_key_long(),
	});

	let groups = $derived([
		{
			name: i18n.account_section_identity(),
			danger: false,
			rows: [
				{
					key: "profile" as RowKey,
					Icon: User,
					label: i18n.common_profile(),
					value: auth.user?.display_name || auth.user?.email || "",
				},
				{
					key: "password" as RowKey,
					Icon: LockKeyhole,
					label: i18n.common_password(),
					value: "",
				},
			],
		},
		{
			name: i18n.account_section_prefs(),
			danger: false,
			rows: [
				{
					key: "language" as RowKey,
					Icon: Globe,
					label: i18n.account_language(),
					value: endonym(getLocale()),
				},
			],
		},
		{
			name: i18n.account_section_devices(),
			danger: false,
			rows: [
				{
					key: "keys" as RowKey,
					Icon: KeyRound,
					label: i18n.account_api_keys(),
					value: keys.data
						? i18n.account_n_keys({ n: keys.data.length })
						: "",
				},
				{
					key: "sessions" as RowKey,
					Icon: Monitor,
					label: i18n.account_active_sessions(),
					value: sessions.data
						? i18n.account_n_active({ n: sessions.data.length })
						: "",
				},
			],
		},
		...(isAdmin
			? [
					{
						name: i18n.account_section_danger(),
						danger: true,
						rows: [
							{
								key: "jwt" as RowKey,
								Icon: RefreshCw,
								label: i18n.account_rotate_key_long(),
								value: "",
							},
						],
					},
				]
			: []),
	]);
</script>

<nav class="lg:hidden">
	{#each groups as g (g.name)}
		<div class="flex items-center gap-2.5 pb-2 pt-5">
			<h2
				class={cn(
					"font-mono text-[11px] font-semibold uppercase tracking-[0.14em]",
					g.danger ? "text-status-failed" : "text-fg-muted",
				)}
			>
				{g.name}
			</h2>
			<span
				class={cn("h-px flex-1", g.danger ? "bg-status-failed/30" : "bg-border")}
				aria-hidden="true"
			></span>
		</div>
		<ul
			class="-mx-4 divide-y divide-border border-y border-border bg-bg-elevated sm:mx-0 sm:overflow-hidden sm:rounded-lg sm:border"
		>
			{#each g.rows as r (r.key)}
				<li>
					<button
						type="button"
						onclick={() => (openRow = r.key)}
						class="flex min-h-[56px] w-full items-center gap-3 px-4 py-3 text-left transition active:bg-bg-hover"
					>
						<r.Icon
							size={18}
							class={cn(
								"shrink-0",
								g.danger ? "text-status-failed" : "text-fg-muted",
							)}
							aria-hidden="true"
						/>
						<span
							class={cn(
								"min-w-0 flex-1 truncate text-[15px]",
								g.danger ? "text-status-failed" : "text-fg",
							)}
						>
							{r.label}
						</span>
						{#if r.value}
							<span class="min-w-0 shrink truncate text-[13px] text-fg-subtle">
								{r.value}
							</span>
						{/if}
						<ChevronRight
							size={16}
							class="shrink-0 text-fg-faint"
							aria-hidden="true"
						/>
					</button>
				</li>
			{/each}
		</ul>
	{/each}
</nav>

<FullScreenPanel
	open={openRow !== null}
	title={openRow ? titles[openRow] : ""}
	onClose={() => (openRow = null)}
>
	{#if openRow === "profile"}
		<ProfileCard />
	{:else if openRow === "password"}
		<PasswordCard />
	{:else if openRow === "language"}
		<LanguageCard />
	{:else if openRow === "keys"}
		<APIKeysCard />
	{:else if openRow === "sessions"}
		<SessionsCard />
	{:else if openRow === "jwt"}
		<JWTRotateCard />
	{/if}
</FullScreenPanel>
