<script lang="ts">
	import { formatDate } from "@lib/dates";
	import PeopleGrid from "@components/shared/PeopleGrid.svelte";
	import InfoCard, { type InfoLink, type InfoRow } from "@components/shared/InfoCard.svelte";
	import {
		CREDIT_ROLES,
		countryName,
		creditRoleLabel,
		mediumLabel,
		personHref,
		personPhoto,
		type Person,
		type Release,
	} from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What the liner notes say, under the tracklist: who guests on which
	// track, who made the record, and the release's own facts. Anyone who is
	// an artist in the library links to their page.
	// `scope` keeps the ids apart: the phone accordion and the md panel both
	// render the open release, one of them hidden.
	let { release: r, scope = "panel" }: { release: Release; scope?: string } = $props();
	let uid = $derived(`${scope}-${r.id}`);

	const ln = (p: Person): InfoLink => ({ text: p.name, href: personHref(p) });

	let guests = $derived.by(() => {
		const by = new Map<string, { person: Person; tracks: string[] }>();
		for (const t of r.tracks ?? [])
			for (const p of t.featuring ?? []) {
				const x = by.get(p.name) ?? { person: p, tracks: [] };
				x.tracks.push(t.title);
				by.set(p.name, x);
			}
		return [...by.values()].map((x) => ({
			key: x.person.name,
			name: x.person.name,
			photo_url: personPhoto(x.person),
			href: personHref(x.person),
			role: x.tracks.join(", "),
		}));
	});

	let credits = $derived.by<InfoRow[]>(() => {
		const rows: InfoRow[] = [];
		for (const role of CREDIT_ROLES) {
			const ps = (r.credits ?? []).filter((c) => c.role === role);
			if (ps.length) rows.push({ label: creditRoleLabel(role), value: ps.map(ln) });
		}
		const players = [...(r.personnel ?? [])].sort((a, b) => Number(!!a.guest) - Number(!!b.guest));
		if (players.length) {
			rows.push({ group: i18n.music_musicians() });
			for (const p of players)
				rows.push({
					label: ln(p),
					value: p.guest ? `${p.instruments.join(", ")} · ${i18n.music_guest()}` : p.instruments.join(", "),
					tone: "text-fg-muted",
					mono: false,
				});
		}
		return rows;
	});

	let facts = $derived.by<InfoRow[]>(() => {
		const rows: InfoRow[] = r.release_date ? [{ label: i18n.detail_released(), value: formatDate(r.release_date) }] : [];
		if (r.label) rows.push({ label: i18n.music_fact_label(), value: r.label, mono: false });
		if (r.catalog_number) rows.push({ label: i18n.music_fact_catalog(), value: r.catalog_number });
		if (r.media?.length) rows.push({ label: i18n.music_fact_media(), value: r.media.map(mediumLabel).join(" · ") });
		if (r.country) rows.push({ label: i18n.music_fact_country(), value: countryName(r.country), mono: false });
		if (r.studio) rows.push({ label: i18n.music_fact_studio(), value: r.studio, mono: false });
		if (r.mbid) rows.push({ label: "MusicBrainz", value: [{ text: r.mbid.slice(0, 8), href: `https://musicbrainz.org/release-group/${r.mbid}`, external: true }], mono: true });
		return rows;
	});
	const h3 = "mb-2 font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint";
</script>

<div class="flex flex-col gap-4">
	{#if guests.length}
		<section aria-labelledby="{uid}-guests">
			<h3 id="{uid}-guests" class={h3}>{i18n.music_featured_artists()}</h3>
			<PeopleGrid people={guests} layout="list" round />
		</section>
	{/if}
	<div class="grid items-start gap-4 xl:grid-cols-2">
		{#if credits.length}
			<InfoCard id="{uid}-credits" title={i18n.music_credits()} rows={credits} />
		{/if}
		<InfoCard id="{uid}-facts" title={i18n.music_fact_release()} rows={facts} />
	</div>
</div>
