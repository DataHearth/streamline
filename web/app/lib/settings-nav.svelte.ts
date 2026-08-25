import {
	SlidersHorizontal,
	Wrench,
	Search,
	Download,
	Cast,
	Gauge,
	Tags,
	Tv,
	Film,
	Clock,
	Shield,
	KeyRound,
	Users,
} from "@lucide/svelte";
import { createQuery } from "@tanstack/svelte-query";
import { auth } from "./auth.svelte";
import { api } from "./api";
import type {
	CustomFormat,
	DownloadClient,
	Indexer,
	MediaServer,
	QualityProfileFull,
	ScheduleList,
	UserList,
} from "./types";
import { m as i18n } from "./paraglide/messages.js";

export type SettingsItem = {
	path: string;
	Icon: typeof SlidersHorizontal;
	label: string;
	// Shown as the row's trailing value on the index list and in the sidebar.
	count?: number | undefined;
};
export type SettingsGroup = { name: string; items: SettingsItem[] };

// Section titles for the drill-in top bar, keyed by path. Static so a page can
// name itself without instantiating the count queries.
export const SETTINGS_TITLES: Record<string, () => string> = {
	"/settings/general": () => i18n.settings_general(),
	"/settings/advanced": () => i18n.settings_advanced(),
	"/settings/quality-profiles": () => i18n.settings_quality_profiles(),
	"/settings/custom-formats": () => i18n.settings_custom_formats(),
	"/settings/series": () => i18n.settings_series(),
	"/settings/media-probe": () => i18n.settings_media_probe(),
	"/settings/indexers": () => i18n.settings_indexers(),
	"/settings/download-clients": () => i18n.settings_download_clients(),
	"/settings/media-servers": () => i18n.settings_media_servers(),
	"/settings/schedules": () => i18n.settings_schedules(),
	"/settings/auth": () => i18n.settings_authentication(),
	"/settings/oidc": () => i18n.settings_sso(),
	"/settings/users": () => i18n.settings_users(),
};

// createSettingsNav builds the grouped section list shared by the desktop
// sidebar and the touch index page. The count queries are the same ones the
// destination pages run, so opening the index warms their cache rather than
// costing an extra round trip.
//
// `withCounts` is off for the sidebar's own render path only in the sense that
// both surfaces want them; it exists so a caller that just needs labels (a
// breadcrumb) doesn't fire six requests.
export function createSettingsNav(withCounts = true) {
	let isAdmin = $derived(auth.user?.role === "admin");

	const indexers = createQuery<Indexer[]>(() => ({
		queryKey: ["indexers"],
		queryFn: () => api<Indexer[]>("/indexers"),
		enabled: withCounts,
	}));
	const downloadClients = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
		enabled: withCounts,
	}));
	const mediaServers = createQuery<{ items: MediaServer[] }>(() => ({
		queryKey: ["media-servers"],
		queryFn: () => api<{ items: MediaServer[] }>("/media-servers"),
		enabled: withCounts,
	}));
	const profiles = createQuery<QualityProfileFull[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfileFull[]>("/quality-profiles"),
		enabled: withCounts,
	}));
	const customFormats = createQuery<CustomFormat[]>(() => ({
		queryKey: ["custom-formats"],
		queryFn: () => api<CustomFormat[]>("/custom-formats"),
		enabled: withCounts,
	}));
	const schedules = createQuery<ScheduleList>(() => ({
		queryKey: ["schedules"],
		queryFn: () => api<ScheduleList>("/schedules"),
		enabled: withCounts,
	}));
	// /users is admin-only; asking as a member is a guaranteed 403.
	const users = createQuery<UserList>(() => ({
		queryKey: ["users", { q: "", role: "", sort: "created", order: "desc", offset: 0, limit: 25 }],
		queryFn: () =>
			api<UserList>("/users?limit=25&offset=0&sort=created&order=desc"),
		enabled: withCounts && !auth.loading && auth.user?.role === "admin",
	}));

	let groups = $derived.by<SettingsGroup[]>(() => {
		const base: SettingsGroup[] = [
			{
				name: i18n.settings_system(),
				items: [
					{
						path: "/settings/general",
						Icon: SlidersHorizontal,
						label: i18n.settings_general(),
					},
					{
						path: "/settings/advanced",
						Icon: Wrench,
						label: i18n.settings_advanced(),
					},
				],
			},
			{
				name: i18n.nav_library(),
				items: [
					{
						path: "/settings/quality-profiles",
						Icon: Gauge,
						label: i18n.settings_quality_profiles(),
						count: profiles.data?.length,
					},
					// Counts what the operator can act on: the shipped library is a
					// constant, so including builtins would show the same 13 on every
					// install and never move.
					{
						path: "/settings/custom-formats",
						Icon: Tags,
						label: i18n.settings_custom_formats(),
						count: customFormats.data?.filter((f) => !f.builtin).length,
					},
					{
						path: "/settings/series",
						Icon: Tv,
						label: i18n.settings_series(),
					},
					// Probing is what happens to files entering the library, so it
					// belongs here rather than under Connections — there is no host to
					// reach and nothing to test a connection to.
					{
						path: "/settings/media-probe",
						Icon: Film,
						label: i18n.settings_media_probe(),
					},
				],
			},
			{
				name: i18n.settings_connections(),
				items: [
					{
						path: "/settings/indexers",
						Icon: Search,
						label: i18n.settings_indexers(),
						count: indexers.data?.length,
					},
					{
						path: "/settings/download-clients",
						Icon: Download,
						label: i18n.settings_download_clients(),
						count: downloadClients.data?.length,
					},
					{
						path: "/settings/media-servers",
						Icon: Cast,
						label: i18n.settings_media_servers(),
						count: mediaServers.data?.items.length,
					},
				],
			},
			{
				name: i18n.settings_automation(),
				items: [
					{
						path: "/settings/schedules",
						Icon: Clock,
						label: i18n.settings_schedules(),
						count: schedules.data?.items.length,
					},
				],
			},
		];
		if (isAdmin) {
			base.push({
				name: i18n.settings_security(),
				items: [
					{
						path: "/settings/auth",
						Icon: Shield,
						label: i18n.settings_authentication(),
					},
					{
						path: "/settings/oidc",
						Icon: KeyRound,
						label: i18n.settings_sso(),
					},
					{
						path: "/settings/users",
						Icon: Users,
						label: i18n.settings_users(),
						count: users.data?.total,
					},
				],
			});
		}
		return base;
	});

	return {
		get groups() {
			return groups;
		},
	};
}
