// Music and books: the wire types (api/openapi.yaml) and the small derivations
// their pages share.

import * as v from "valibot";
import { m as i18n } from "./paraglide/messages.js";
import { getLocale } from "./paraglide/runtime.js";
import { formatDate } from "./dates";
import { formatBytes } from "./format";
import { authorPosterUrl, artistPosterUrl } from "./posters";

export type MediaState = "available" | "wanted" | "downloading";

// ── Music ────────────────────────────────────────────────────────────────
export type ReleaseType = "album" | "ep" | "single" | "compilation" | "live" | "other";
export type ReleaseStatus = MediaState | "upcoming" | "paused" | "skipped";
// What following an artist means. "all" is the default: every release, past
// and future.
export type ArtistMonitor = "all" | "future" | "manual" | "none";

// ── People ───────────────────────────────────────────────────────────────
// Someone credited on a release, a track or a book. `artist_id` is set when
// they are an artist in the music library, `author_id` when they are an author
// row of the book library. `shelf` is derived on the client for a creator role
// (the author name the shelf filters by); either link makes them a link, and
// either id is what their photo is built from.
export type Person = { name: string; mbid?: string; artist_id?: number; author_id?: number; shelf?: string };

// A group's line-up as MusicBrainz dates it. `to` is set for a former member.
export type Member = Person & { instruments: string[]; from?: number; to?: number };
// Who played on a release. `guest` marks a player from outside a group.
export type Performer = Person & { instruments: string[]; guest?: boolean };
// MusicBrainz relationship types, folded to the five liner notes carry.
export type CreditRole = "producer" | "recording" | "mix" | "mastering" | "artwork";
export type Credit = Person & { role: CreditRole };
export type MediumFormat = "cd" | "vinyl" | "digital" | "cassette";

export type Track = {
	id: number;
	disc: number;
	number: number;
	title: string;
	// The artist credit's "feat." part, one entry per guest.
	featuring?: Person[];
	writers?: Person[];
	duration: number;
	bonus?: boolean;
	has_file: boolean;
};

export type Release = {
	id: number;
	artist_id: number;
	title: string;
	type: ReleaseType;
	release_date?: string | null;
	label?: string;
	status: ReleaseStatus;
	monitored: boolean;
	track_count: number;
	tracks_have: number;
	// True until the album's tracks have been fetched.
	tracks_pending: boolean;
	quality?: MusicTier;
	format?: string;
	size?: number;
	duration?: number;
	progress?: number;
	// Detail only; the list leaves them out.
	tracks?: Track[];
	mbid?: string;
	catalog_number?: string;
	// ISO 3166-1 alpha-2.
	country?: string;
	media?: MediumFormat[];
	studio?: string;
	credits?: Credit[];
	personnel?: Performer[];
};

export type Artist = {
	id: number;
	mbid?: string;
	name: string;
	type?: "group" | "person";
	// A group's line-up, current and former. Detail only.
	members?: Member[];
	genre?: string;
	origin?: string;
	since?: number;
	overview?: string;
	// Attribution for the overview: the Wikipedia page it came from.
	overview_source?: string;
	monitor: ArtistMonitor;
	// A music or book profile name; empty is the server default.
	quality_profile?: string;
	status: MediaState;
	added_at: string;
	album_count: number;
	tracks_have: number;
	size: number;
	// True while any album is still fetching its tracks or credits; the detail
	// is polled until it clears.
	hydrating: boolean;
	// Newest first, upcoming included.
	albums: Release[];
};

export type ArtistList = { items: Artist[]; total: number; page: number; limit: number };
export type ArtistCounts = {
	total: number;
	status_total: number;
	wanted: number;
	downloading: number;
	available: number;
	monitored_total: number;
	monitored: number;
	unmonitored: number;
	albums: number;
};

// ── Books ────────────────────────────────────────────────────────────────
export type BookKind = "novel" | "bd" | "comic" | "manga";
export type BookFormat = "ebook" | "audiobook";
export type BookMonitor = "both" | "ebook" | "audiobook" | "none";
export type FormatState = MediaState | "unmonitored";

export type Edition = {
	id: number;
	language: string;
	title: string;
	publisher: string;
	year: number;
	format: BookFormat;
	original?: boolean;
	pages?: number;
	narrator?: string;
	duration?: number;
	translator?: string;
};

// One per format. A book is one entry; its ebook and its audiobook are two
// slots on it, each pointing at the edition it wants.
export type FormatSlot = {
	format: BookFormat;
	state: FormatState;
	edition_id: number | null;
	file?: { container: string; size: number; file_count?: number };
	progress?: number;
	// Set while the file on disk is another edition's, kept until the new one
	// has downloaded.
	replacing?: { language: string };
};

// Translators and narrators stay on the edition they worked on; this is
// everyone else, and the edition's language when the credit is tied to one.
export type BookRole = "author" | "writer" | "artist" | "colorist" | "cover" | "translator" | "narrator";
export type BookCredit = Person & { role: BookRole; language?: string; author_id: number };

export type Book = {
	id: number;
	hardcover_id?: number;
	// Hardcover's community average, out of 5.
	rating?: number;
	contributors?: BookCredit[];
	title: string;
	original_title?: string;
	author: string;
	kind: BookKind;
	genre?: string;
	first_published: number;
	overview?: string;
	status: MediaState;
	monitor: BookMonitor;
	// A music or book profile name; empty is the server default.
	quality_profile?: string;
	// The reader's language: which edition a new slot picks first.
	preferred_language: string;
	added_at: string;
	formats: FormatSlot[];
	editions: Edition[];
	series?: { id: number; title: string; number: number };
};

export type Volume = {
	// The volume's own book id: its cover is that book's poster.
	id: number;
	number: number;
	status: MediaState | "upcoming";
	release_date?: string;
};

export type SeriesMonitor = "all" | "future" | "none";

export type BookSeries = {
	id: number;
	hardcover_id?: number;
	rating?: number;
	contributors?: BookCredit[];
	title: string;
	original_title?: string;
	author: string;
	kind: BookKind;
	status: MediaState;
	ongoing: boolean;
	since: number;
	overview?: string;
	monitor: SeriesMonitor;
	// A music or book profile name; empty is the server default.
	quality_profile?: string;
	edition: string;
	editions: string[];
	added_at: string;
	// True while any volume is still a stub; the detail is polled until it clears.
	hydrating: boolean;
	volumes: Volume[];
};

// One cover on a shelf: a standalone book, or a whole series folded into one
// stacked card.
export type ShelfItem = {
	type: "book" | "series";
	id: number;
	title: string;
	author: string;
	kind: BookKind;
	status: MediaState;
	// The id of the book whose poster is the cover (a series': its first volume).
	cover_id: number;
	added_at: string;
	year?: number;
	progress?: number;
	formats?: { format: BookFormat; state: FormatState }[];
	volumes_have?: number;
	volumes_out?: number;
	quality_profile?: string;
};

export type ShelfList = { items: ShelfItem[]; total: number; page: number; limit: number };
export type BookCounts = {
	total: number;
	status_total: number;
	available: number;
	wanted: number;
	downloading: number;
	author_total: number;
	authors: { name: string; count: number }[];
	format_total: number;
	ebook: number;
	audiobook: number;
};

// ── Music helpers ────────────────────────────────────────────────────────
// Empty for an undated album, so a caller can join it away.
// The Wikipedia overview comes back in the UI's language, English when that
// language has no article.
export const overviewLang = () => (getLocale() === "fr" ? "fr" : "en");

export const releaseYear = (r: Release) => r.release_date?.slice(0, 4) ?? "";

export function releaseTypeLabel(t: ReleaseType): string {
	if (t === "ep") return i18n.music_type_ep();
	if (t === "single") return i18n.music_type_single();
	if (t === "compilation") return i18n.music_type_compilation();
	if (t === "live") return i18n.music_type_live();
	if (t === "other") return i18n.music_type_other();
	return i18n.music_type_album();
}

export const multiDisc = (r: Release) => new Set((r.tracks ?? []).map((t) => t.disc)).size > 1;

// "2019 · Album", or just the type when the album has no date.
export const releaseLine = (r: Release) => [releaseYear(r), releaseTypeLabel(r.type)].filter(Boolean).join(" · ");

export function qualityLabel(q?: MusicTier): string {
	if (q === "hires") return i18n.qp_tier_hires();
	if (q === "low") return i18n.qp_tier_low();
	if (q === "lossless") return i18n.music_quality_lossless();
	if (q === "high") return i18n.music_quality_high();
	if (q === "standard") return i18n.music_quality_standard();
	return "";
}

const n = (v: number) => v.toLocaleString();

export const tracksCount = (count: number) =>
	(count === 1 ? i18n.music_tracks_one : i18n.music_tracks_other)({ count: n(count) });

export const releasesCount = (count: number) =>
	(count === 1 ? i18n.music_releases_one : i18n.music_releases_other)({ count: n(count) });

export const trackTime = (s: number) =>
	`${Math.floor(s / 60)}:${String(Math.round(s % 60)).padStart(2, "0")}`;

// A partial copy counts what is missing rather than saying "Wanted", which
// reads as if nothing were there.
export function releasePill(r: Release): { token: string; label: string; live?: boolean } {
	if (r.status === "upcoming") return { token: "unaired", label: i18n.music_upcoming() };
	if (r.status === "paused") return { token: "downloading", label: i18n.status_paused(), live: true };
	if (r.status === "downloading")
		return {
			token: "downloading",
			label: `${i18n.status_downloading()} · ${Math.round(r.progress ?? 0)}%`,
			live: true,
		};
	if (r.status === "wanted" || r.status === "skipped") {
		const missing = r.track_count - r.tracks_have;
		if (r.tracks_have > 0)
			return {
				token: "wanted",
				label: (missing === 1 ? i18n.music_tracks_missing_one : i18n.music_tracks_missing_other)({
					count: n(missing),
				}),
			};
		return { token: "wanted", label: i18n.status_wanted() };
	}
	return { token: "available", label: i18n.status_available() };
}

export function releaseFacts(r: Release): string[] {
	if (r.status === "upcoming")
		return [
			i18n.music_out_on({ date: formatDate(r.release_date) }),
			i18n.music_tracks_announced({ count: n(r.track_count) }),
		];
	const p: string[] = r.tracks_pending ? [] : [tracksCount(r.track_count)];
	if (r.duration && !r.tracks_pending) p.push(i18n.music_minutes({ count: n(Math.round(r.duration / 60)) }));
	if (r.quality && r.tracks_have > 0) p.push(qualityLabel(r.quality));
	if (r.size) p.push(formatBytes(r.size, ""));
	return p.filter(Boolean);
}

// Stated once above the tracklist instead of on every row.
export function qualityNote(r: Release): string {
	if (!r.quality || !r.tracks_have) return "";
	return i18n.music_all_tracks({
		quality: [qualityLabel(r.quality).toLowerCase(), r.format].filter(Boolean).join(" · "),
	});
}

export function artistTally(a: Artist) {
	const out = a.albums.filter((r) => r.status !== "upcoming");
	return {
		released: out.length,
		have: out.filter((r) => r.status === "available").length,
		wanted: out.filter((r) => r.status === "wanted").length,
		downloading: out.filter((r) => r.status === "downloading" || r.status === "paused").length,
		upcoming: a.albums.length - out.length,
	};
}

export function releaseGroups(releases: Release[]) {
	return {
		albums: releases.filter((r) => r.type === "album"),
		others: releases.filter((r) => r.type !== "album"),
	};
}

// The release an artist page opens on: the newest with something missing,
// else the newest that is out.
export function defaultRelease(releases: Release[]): Release | undefined {
	return (
		releases.find((r) => r.status === "wanted" || r.status === "downloading" || r.status === "paused") ??
		releases.find((r) => r.status !== "upcoming") ??
		releases[0]
	);
}

// ── People helpers ───────────────────────────────────────────────────────
export function personHref(p: Person): string | undefined {
	if (p.artist_id) return `/music/${p.artist_id}`;
	if (p.shelf) return `/books?author=${encodeURIComponent(p.shelf)}`;
	return undefined;
}

export function peopleList(ps: Person[]): string {
	const names = ps.map((p) => p.name);
	try {
		return new Intl.ListFormat(getLocale(), { type: "conjunction" }).format(names);
	} catch {
		return names.join(", ");
	}
}

export const memberYears = (m: Member) =>
	m.to ? (m.from ? `${m.from}–${m.to}` : `–${m.to}`) : m.from ? i18n.music_since({ year: String(m.from) }) : "";

// A person shows art only when the library holds them: an artist row for music
// people, an author row for the creators of a book. Everyone else is a monogram.
export function personPhoto(p: Person): string | undefined {
	if (p.artist_id) return artistPosterUrl(p.artist_id);
	if (p.author_id && p.shelf) return authorPosterUrl(p.author_id);
	return undefined;
}

export const CREDIT_ROLES: CreditRole[] = ["producer", "recording", "mix", "mastering", "artwork"];
export function creditRoleLabel(r: CreditRole): string {
	if (r === "producer") return i18n.music_role_producer();
	if (r === "recording") return i18n.music_role_recording();
	if (r === "mix") return i18n.music_role_mix();
	if (r === "mastering") return i18n.music_role_mastering();
	return i18n.music_role_artwork();
}

export function mediumLabel(m: MediumFormat): string {
	if (m === "cd") return i18n.music_medium_cd();
	if (m === "vinyl") return i18n.music_medium_vinyl();
	if (m === "cassette") return i18n.music_medium_cassette();
	return i18n.music_medium_digital();
}

export function countryName(code: string): string {
	try {
		return new Intl.DisplayNames([getLocale()], { type: "region" }).of(code.toUpperCase()) ?? code;
	} catch {
		return code.toUpperCase();
	}
}

// One person across a whole discography: how many releases they are on, and
// what they did there.
export type Reach = { person: Person; releases: number; tracks: number; roles: CreditRole[]; instruments: string[] };
export type ArtistPeople = { current: Member[]; former: Member[]; featured: Reach[]; musicians: Reach[]; production: Reach[]; total: number };

// Everyone credited across an artist's releases, folded per person. The band
// itself (and a solo artist) is left out of the outside credits: producing
// your own record is not a collaboration.
export function artistPeople(a: Artist): ArtistPeople {
	const members = a.members ?? [];
	const band = new Set([a.name, ...members.map((m) => m.name)]);
	type Acc = { person: Person; releases: Set<number>; tracks: number; roles: Set<CreditRole>; instruments: Set<string> };
	const featured = new Map<string, Acc>();
	const musicians = new Map<string, Acc>();
	const production = new Map<string, Acc>();
	const at = (map: Map<string, Acc>, p: Person) => {
		let x = map.get(p.name);
		if (!x) map.set(p.name, (x = { person: p, releases: new Set(), tracks: 0, roles: new Set(), instruments: new Set() }));
		return x;
	};
	for (const r of a.albums) {
		for (const t of r.tracks ?? [])
			for (const p of t.featuring ?? []) {
				const x = at(featured, p);
				x.tracks++;
				x.releases.add(r.id);
			}
		for (const p of r.personnel ?? [])
			if (!band.has(p.name)) {
				const x = at(musicians, p);
				p.instruments.forEach((i) => x.instruments.add(i));
				x.releases.add(r.id);
			}
		for (const c of r.credits ?? [])
			if (!band.has(c.name)) {
				const x = at(production, c);
				x.roles.add(c.role);
				x.releases.add(r.id);
			}
	}
	const out = (map: Map<string, Acc>): Reach[] =>
		[...map.values()]
			.map((x) => ({
				person: x.person,
				releases: x.releases.size,
				tracks: x.tracks,
				roles: CREDIT_ROLES.filter((r) => x.roles.has(r)),
				instruments: [...x.instruments],
			}))
			.sort((x, y) => y.releases - x.releases || y.tracks - x.tracks || x.person.name.localeCompare(y.person.name));
	const p = {
		current: members.filter((m) => !m.to),
		former: members.filter((m) => !!m.to),
		featured: out(featured),
		musicians: out(musicians),
		production: out(production),
	};
	const names = new Set([...members.map((m) => m.name), ...[...p.featured, ...p.musicians, ...p.production].map((x) => x.person.name)]);
	return { ...p, total: names.size };
}

export const reachRole = (x: Reach) =>
	[...x.roles.map(creditRoleLabel), ...(x.tracks ? [i18n.music_role_featured()] : []), ...(x.instruments.length ? [x.instruments.join(", ")] : [])].join(" · ");

// The overview's short list: whoever is on the most releases, whatever they
// did there.
export function collaborators(p: ArtistPeople, n: number): Reach[] {
	const by = new Map<string, Reach>();
	for (const x of [...p.production, ...p.featured, ...p.musicians]) {
		const y = by.get(x.person.name);
		if (!y) by.set(x.person.name, { ...x, roles: [...x.roles], instruments: [...x.instruments] });
		else {
			y.releases = Math.max(y.releases, x.releases);
			y.tracks += x.tracks;
			y.roles = CREDIT_ROLES.filter((r) => y.roles.includes(r) || x.roles.includes(r));
			y.instruments = [...new Set([...y.instruments, ...x.instruments])];
		}
	}
	return [...by.values()].sort((x, y) => y.releases - x.releases || y.tracks - x.tracks).slice(0, n);
}

// ── Book helpers ─────────────────────────────────────────────────────────
export const BOOK_ROLES: BookRole[] = ["author", "writer", "artist", "colorist", "cover", "translator", "narrator"];
export function bookRoleLabel(r: BookRole): string {
	if (r === "author") return i18n.books_author();
	if (r === "writer") return i18n.books_role_writer();
	if (r === "artist") return i18n.books_role_artist();
	if (r === "colorist") return i18n.books_role_colorist();
	if (r === "cover") return i18n.books_role_cover();
	if (r === "translator") return i18n.books_role_translator();
	return i18n.books_role_narrator();
}

const CREATOR_ROLES = new Set<BookRole>(["author", "writer", "artist"]);

// The wire credit carries the role and the author row; the shelf filter and the
// photo are for the roles that write the book.
export const creditPerson = (c: BookCredit): Person => ({
	name: c.name,
	author_id: c.author_id,
	shelf: CREATOR_ROLES.has(c.role) ? c.name : undefined,
});

export type BookPerson = { person: Person; role: BookRole; languages: string[] };

// Everyone credited on a book: its makers first, then the translators and
// narrators of the editions in use, then those of the other editions. The
// same person in the same role on two editions is one entry with both
// languages. A narrator who is also the author keeps the author's link.
export function bookPeople(credits: BookCredit[], editions: Edition[] = [], inUse: (number | null)[] = []): BookPerson[] {
	const makers = new Map(credits.map((c) => [c.name, creditPerson(c)]));
	const out = new Map<string, BookPerson>();
	const add = (p: Person, role: BookRole, language?: string) => {
		const k = `${role}:${p.name}`;
		const x = out.get(k) ?? { person: p, role, languages: [] };
		if (language && !x.languages.includes(language)) x.languages.push(language);
		out.set(k, x);
	};
	for (const c of [...credits].sort((a, b) => BOOK_ROLES.indexOf(a.role) - BOOK_ROLES.indexOf(b.role)))
		add(creditPerson(c), c.role, c.language);
	const sorted = [...editions].sort((a, b) => Number(inUse.includes(b.id)) - Number(inUse.includes(a.id)));
	for (const e of sorted) {
		const known = (name: string): Person => makers.get(name) ?? { name };
		if (e.translator) add(known(e.translator), "translator", e.language);
		if (e.narrator) add(known(e.narrator), "narrator", e.language);
	}
	return [...out.values()];
}

export function formatLabel(f: BookFormat): string {
	return f === "ebook" ? i18n.books_format_ebook() : i18n.books_format_audiobook();
}

export function kindLabel(k: BookKind): string {
	if (k === "bd") return i18n.books_kind_bd();
	if (k === "comic") return i18n.books_kind_comic();
	if (k === "manga") return i18n.books_kind_manga();
	return i18n.books_kind_novel();
}

// Language names come from the platform, in the UI's language. Mid-sentence
// they keep the platform's casing ("French", "français"); standalone ones
// — a tooltip, a chip's label — take a capital.
export function languageName(code: string, standalone = false): string {
	try {
		const name = new Intl.DisplayNames([getLocale()], { type: "language" }).of(code);
		if (name) return standalone ? name.charAt(0).toUpperCase() + name.slice(1) : name;
	} catch {
		/* engines without DisplayNames fall through to the code */
	}
	return code.toUpperCase();
}

export function listenTime(s: number): string {
	const h = Math.floor(s / 3600);
	const m = Math.round((s % 3600) / 60);
	return i18n.books_duration({ h: String(h), m: String(m).padStart(2, "0") });
}

export function editionDetail(e: Edition): string {
	const p: string[] = [];
	if (e.pages) p.push(i18n.books_pages({ count: n(e.pages) }));
	if (e.narrator) p.push(i18n.books_read_by({ name: e.narrator }));
	if (e.duration) p.push(listenTime(e.duration));
	if (e.translator) p.push(i18n.books_translated_by({ name: e.translator }));
	return p.join(" · ");
}

export const hasLocalEdition = (b: Book, f: BookFormat) =>
	b.editions.some((e) => e.format === f && e.language === b.preferred_language);

export const volumeLabel = (num: number) => i18n.books_volume_n({ n: String(num) });

// ── Book releases (manual search) ────────────────────────────────────────
// A book release's format is its container, and its language the word the
// scene name carries; neither is an audio track, so the video chips do not apply.
const AUDIO_CONTAINERS = new Set(["M4B", "MP3", "M4A", "FLAC", "OGG"]);
export function releaseFormat(source?: string): BookFormat {
	return AUDIO_CONTAINERS.has((source ?? "").toUpperCase()) ? "audiobook" : "ebook";
}
const RELEASE_LANGS: [RegExp, string][] = [
	[/\b(FRENCH|FR|VF)\b/i, "fr"],
	[/\b(ENGLISH|EN|ENG)\b/i, "en"],
	[/\b(SPANISH|ES|ESP)\b/i, "es"],
	[/\b(GERMAN|DE)\b/i, "de"],
	[/\b(ITALIAN|IT)\b/i, "it"],
	[/\b(JAPANESE|JP|JA)\b/i, "ja"],
];
export function releaseLanguage(title: string): string | null {
	const t = title.replace(/[._]/g, " ");
	for (const [re, code] of RELEASE_LANGS) if (re.test(t)) return code;
	return null;
}

// ── Quality profiles ─────────────────────────────────────────────────────
// Music and books keep their own profiles, served by /music/quality-profiles
// and /books/quality-profiles: a resolution or a video codec means nothing to a
// FLAC or an EPUB. Names are unique per medium.
export type ProfileMedia = "music" | "books";
export const profilesPath = (media: ProfileMedia) => `/${media}/quality-profiles`;

// Best first. A profile grabs only the tiers it ticks and keeps upgrading until
// it holds `preferred`. Leaving hi-res unticked is how a Lossless profile keeps
// 24-bit files off the disk.
export type MusicTier = "hires" | "lossless" | "high" | "standard" | "low";
export const MUSIC_TIERS: MusicTier[] = ["hires", "lossless", "high", "standard", "low"];

export type MusicProfile = {
	name: string;
	is_default?: boolean;
	tiers: MusicTier[];
	preferred: MusicTier;
	upgrade_allowed: boolean;
};

// Comics and manga are ebooks here too: they arrive as CBZ or CBR.
export type EbookFormat = "EPUB" | "AZW3" | "MOBI" | "PDF" | "CBZ" | "CBR";
export type AudiobookFormat = "M4B" | "MP3" | "M4A" | "FLAC";
export const EBOOK_FORMATS: EbookFormat[] = ["EPUB", "AZW3", "MOBI", "PDF", "CBZ", "CBR"];
export const AUDIOBOOK_FORMATS: AudiobookFormat[] = ["M4B", "MP3", "M4A", "FLAC"];
// kbps. 0 sets no floor.
export const AUDIOBOOK_BITRATES = [0, 64, 96, 128];

// One profile covers both slots of a book, the way one entry holds its ebook
// and its audiobook.
export type BookProfile = {
	name: string;
	// The kinds of book this profile is the default for.
	default_for: BookKind[];
	upgrade_allowed: boolean;
	ebook: { formats: EbookFormat[]; preferred: EbookFormat };
	audiobook: { formats: AudiobookFormat[]; preferred: AudiobookFormat; min_bitrate: number };
};

const TIER_FORMATS: Record<MusicTier, string> = {
	hires: "FLAC · ALAC 24-bit",
	lossless: "FLAC · ALAC 16-bit",
	high: "MP3 320 · V0 · AAC 256",
	standard: "MP3 192–256 · AAC 192–255",
	low: "MP3 · AAC < 192 kbps",
};
// A typical 45-minute album, so the list can say what each step costs on disk.
const TIER_ALBUM_BYTES: Record<MusicTier, number> = {
	hires: 1.3e9,
	lossless: 330e6,
	high: 110e6,
	standard: 75e6,
	low: 45e6,
};

export function tierLabel(t: MusicTier): string {
	if (t === "hires") return i18n.qp_tier_hires();
	if (t === "lossless") return i18n.music_quality_lossless();
	if (t === "high") return i18n.music_quality_high();
	if (t === "standard") return i18n.music_quality_standard();
	return i18n.qp_tier_low();
}
export const tierFormats = (t: MusicTier) => TIER_FORMATS[t];
export const tierAlbumSize = (t: MusicTier) =>
	i18n.qp_tier_album({ size: formatBytes(TIER_ALBUM_BYTES[t], "") });

// A search result's tier, read off the container and bit depth its source names
// ("FLAC 24/96", "MP3 V0"): the buckets a profile ticks, so a result says where
// it falls in the profile it is judged against.
export function releaseTier(source?: string): MusicTier {
	const s = (source ?? "").toUpperCase();
	if (/\b(FLAC|ALAC|WAV)\b/.test(s)) return /\b24\b/.test(s) ? "hires" : "lossless";
	if (/\b(V0|320)\b/.test(s) || /\bAAC 256\b/.test(s)) return "high";
	const kbps = Number(/\b(\d{3})\b/.exec(s)?.[1] ?? 0);
	return kbps >= 192 || /\bV2\b/.test(s) ? "standard" : "low";
}

export const bitrateLabel = (kbps: number) =>
	kbps ? i18n.qp_bitrate_kbps({ count: String(kbps) }) : i18n.qp_bitrate_any();

// Ticked order is not preference order: both lists always read best first.
export const sortTiers = (ts: MusicTier[]) => MUSIC_TIERS.filter((t) => ts.includes(t));
export function sortFormats<T extends string>(all: readonly T[], picked: T[]): T[] {
	return all.filter((f) => picked.includes(f));
}

// The form keeps its values flat — the form layer resolves top-level names
// only — and the settings page maps them to and from the API's nested shape.
export type MusicProfileValues = {
	name: string;
	tiers: MusicTier[];
	preferred: MusicTier;
	upgrade_allowed: boolean;
};
export type BookProfileValues = {
	name: string;
	upgrade_allowed: boolean;
	ebook_formats: EbookFormat[];
	ebook_preferred: EbookFormat;
	audiobook_formats: AudiobookFormat[];
	audiobook_preferred: AudiobookFormat;
	min_bitrate: number;
};

export const MUSIC_PROFILE_DEFAULTS: MusicProfileValues = {
	name: "",
	tiers: ["lossless", "high"],
	preferred: "lossless",
	upgrade_allowed: true,
};
export const BOOK_PROFILE_DEFAULTS: BookProfileValues = {
	name: "",
	upgrade_allowed: true,
	ebook_formats: ["EPUB", "AZW3"],
	ebook_preferred: "EPUB",
	audiobook_formats: ["M4B", "MP3"],
	audiobook_preferred: "M4B",
	min_bitrate: 64,
};

// The forms make the cross-field rules impossible to break (the preferred
// pick only offers what is ticked, the last tick cannot be cleared), so the
// schemas only guard the shape.
export const musicProfileSchema = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	tiers: v.pipe(v.array(v.picklist(MUSIC_TIERS)), v.minLength(1, i18n.validation_required())),
	preferred: v.picklist(MUSIC_TIERS),
	upgrade_allowed: v.boolean(),
});
export const bookProfileSchema = v.object({
	name: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
	upgrade_allowed: v.boolean(),
	ebook_formats: v.pipe(v.array(v.picklist(EBOOK_FORMATS)), v.minLength(1, i18n.validation_pick_format())),
	ebook_preferred: v.picklist(EBOOK_FORMATS),
	audiobook_formats: v.pipe(v.array(v.picklist(AUDIOBOOK_FORMATS)), v.minLength(1, i18n.validation_pick_format())),
	audiobook_preferred: v.picklist(AUDIOBOOK_FORMATS),
	min_bitrate: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(1024)),
});

export const musicValues = (p: MusicProfile): MusicProfileValues => ({
	name: p.name,
	tiers: sortTiers(p.tiers),
	preferred: p.preferred,
	upgrade_allowed: p.upgrade_allowed,
});
export const musicRequest = (x: MusicProfileValues) => ({ ...x, tiers: sortTiers(x.tiers) });

export const bookValues = (p: BookProfile): BookProfileValues => ({
	name: p.name,
	upgrade_allowed: p.upgrade_allowed,
	ebook_formats: sortFormats(EBOOK_FORMATS, p.ebook.formats),
	ebook_preferred: p.ebook.preferred,
	audiobook_formats: sortFormats(AUDIOBOOK_FORMATS, p.audiobook.formats),
	audiobook_preferred: p.audiobook.preferred,
	min_bitrate: p.audiobook.min_bitrate,
});
export const bookRequest = (x: BookProfileValues) => ({
	name: x.name,
	upgrade_allowed: x.upgrade_allowed,
	ebook: { formats: sortFormats(EBOOK_FORMATS, x.ebook_formats), preferred: x.ebook_preferred },
	audiobook: {
		formats: sortFormats(AUDIOBOOK_FORMATS, x.audiobook_formats),
		preferred: x.audiobook_preferred,
		min_bitrate: x.min_bitrate,
	},
});
