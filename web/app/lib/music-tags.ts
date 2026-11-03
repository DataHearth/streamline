// The album tag panel's shapes (GET /music/albums/{id}/tags) and the helpers
// the panel and its overrides editor share.

export type TagOverrides = Record<string, string[]>;
export type TagKeyStatus = "same" | "differs" | "kept";
export type TagFileState = "untagged" | "pending" | "deferred" | "written" | "failed";

export type TagKey = {
	key: string;
	current: string[];
	desired: string[];
	status: TagKeyStatus;
	overridden: boolean;
};

export type TagFile = {
	media_file_id: number;
	track_id: number;
	disc: number;
	position: number;
	title: string;
	file_name: string;
	format: string;
	state: TagFileState;
	error?: string;
	tagged_at?: string;
	read_error?: string;
	overrides: TagOverrides;
	keys: TagKey[];
};

export type AlbumTags = {
	album_id: number;
	mode: "off" | "import" | "managed";
	managed: boolean;
	overrides: TagOverrides;
	files: TagFile[];
};

// One row of the overrides editor: a tag name and its values, written
// separated by semicolons, the way taggers show a multi-valued tag.
export type OverrideRow = { key: string; values: string };

export const VALUE_SEPARATOR = ";";

export function overrideRows(o: TagOverrides): OverrideRow[] {
	return Object.keys(o)
		.sort()
		.map((key) => ({ key, values: (o[key] ?? []).join(`${VALUE_SEPARATOR} `) }));
}

// rowsToOverrides drops a row with no name; a named row with no value is an
// override that removes the tag.
export function rowsToOverrides(rows: OverrideRow[]): TagOverrides {
	const out: TagOverrides = {};
	for (const r of rows) {
		const key = r.key.trim().toUpperCase();
		if (!key) continue;
		out[key] = r.values
			.split(VALUE_SEPARATOR)
			.map((v) => v.trim())
			.filter((v) => v !== "");
	}
	return out;
}

// differing counts the keys a write would change in a file.
export const differing = (f: TagFile) => f.keys.filter((k) => k.status === "differs").length;
