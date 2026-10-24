import {
	SlidersHorizontal,
	Wrench,
	Search,
	Download,
	Cast,
	Gauge,
	Tags,
	Tv,
	Music,
	Film,
	Replace,
	FolderTree,
	Languages,
	Clock,
	Shield,
	KeyRound,
	Users,
	FileText,
} from "@lucide/svelte";
import { createQuery } from "@tanstack/svelte-query";
import { auth } from "./auth.svelte";
import { api } from "./api";
import { profilesPath } from "./music-books";
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

// Every section once, keyed by path: its icon and title. The sidebar and the
// touch index group them, the drill-in top bar names one, and the search
// surfaces list them — none of them needs the count queries for that.
export const SETTINGS_SECTIONS = {
	"/settings/general": { Icon: SlidersHorizontal, title: () => i18n.settings_general() },
	"/settings/logs": { Icon: FileText, title: () => i18n.settings_logs() },
	"/settings/advanced": { Icon: Wrench, title: () => i18n.settings_advanced() },
	"/settings/library": { Icon: FolderTree, title: () => i18n.settings_library() },
	"/settings/quality-profiles": { Icon: Gauge, title: () => i18n.settings_quality_profiles() },
	"/settings/custom-formats": { Icon: Tags, title: () => i18n.settings_custom_formats() },
	"/settings/series": { Icon: Tv, title: () => i18n.settings_series() },
	"/settings/music": { Icon: Music, title: () => i18n.settings_music() },
	"/settings/media-probe": { Icon: Film, title: () => i18n.settings_media_probe() },
	"/settings/transcoding": { Icon: Replace, title: () => i18n.settings_transcoding() },
	"/settings/metadata": { Icon: Languages, title: () => i18n.settings_metadata() },
	"/settings/indexers": { Icon: Search, title: () => i18n.settings_indexers() },
	"/settings/download-clients": { Icon: Download, title: () => i18n.settings_download_clients() },
	"/settings/media-servers": { Icon: Cast, title: () => i18n.settings_media_servers() },
	"/settings/schedules": { Icon: Clock, title: () => i18n.settings_schedules() },
	"/settings/auth": { Icon: Shield, title: () => i18n.settings_authentication() },
	"/settings/oidc": { Icon: KeyRound, title: () => i18n.settings_sso() },
	"/settings/users": { Icon: Users, title: () => i18n.settings_users() },
} satisfies Record<string, { Icon: typeof SlidersHorizontal; title: () => string }>;
type SettingsPath = keyof typeof SETTINGS_SECTIONS;

// Section titles for the drill-in top bar, keyed by path.
export const SETTINGS_TITLES: Record<string, () => string> = Object.fromEntries(
	Object.entries(SETTINGS_SECTIONS).map(([path, s]) => [path, s.title]),
);

function section(path: SettingsPath, count?: number): SettingsItem {
	const { Icon, title } = SETTINGS_SECTIONS[path];
	return { path, Icon, label: title(), count };
}

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
	// Music and books keep their own lists; the row counts all three.
	const musicProfiles = createQuery<unknown[]>(() => ({
		queryKey: ["quality-profiles", "music"],
		queryFn: () => api<unknown[]>(profilesPath("music")),
		enabled: withCounts,
	}));
	const bookProfiles = createQuery<unknown[]>(() => ({
		queryKey: ["quality-profiles", "books"],
		queryFn: () => api<unknown[]>(profilesPath("books")),
		enabled: withCounts,
	}));
	let profileCount = $derived(
		profiles.data
			? profiles.data.length + (musicProfiles.data?.length ?? 0) + (bookProfiles.data?.length ?? 0)
			: undefined,
	);
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
					section("/settings/general"),
					// The application and HTTP access logs: what streamline writes, at
					// what level, where to, and how a log file rotates.
					section("/settings/logs"),
					section("/settings/advanced"),
				],
			},
			{
				name: i18n.nav_library(),
				items: [
					section("/settings/library"),
					section("/settings/quality-profiles", profileCount),
					// Counts what the operator can act on: the shipped library is a
					// constant, so including builtins would show the same 13 on every
					// install and never move.
					section("/settings/custom-formats", customFormats.data?.filter((f) => !f.builtin).length),
					section("/settings/series"),
					section("/settings/music"),
					// Probing is what happens to files entering the library, so it
					// belongs here rather than under Connections — there is no host to
					// reach and nothing to test a connection to.
					section("/settings/media-probe"),
					// Sits next to probing because it is the other thing that happens
					// to a file entering the library, and it reads the same binary.
					// Admin-only, like every transcoding surface.
					...(isAdmin ? [section("/settings/transcoding")] : []),
					// The provider keys and the language they answer in decide what
					// every title in the library is called, so this is a library
					// setting rather than a connection — there is no host to reach.
					section("/settings/metadata"),
				],
			},
			{
				name: i18n.settings_connections(),
				items: [
					section("/settings/indexers", indexers.data?.length),
					section("/settings/download-clients", downloadClients.data?.length),
					section("/settings/media-servers", mediaServers.data?.items.length),
				],
			},
			{
				name: i18n.settings_automation(),
				items: [section("/settings/schedules", schedules.data?.items.length)],
			},
		];
		if (isAdmin) {
			base.push({
				name: i18n.settings_security(),
				items: [
					section("/settings/auth"),
					section("/settings/oidc"),
					section("/settings/users", users.data?.total),
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
