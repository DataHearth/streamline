import { api } from "./api";
import type {
	ApplySourceConfigResult,
	ArrApp,
	ArrConfigSelection,
	ArrPreview,
	ArrProfileMapping,
	ArrProfileTranslation,
	ArrRootCheck,
	ArrRootMapping,
	ArrSourceRequest,
} from "./types";

// Typed wrappers over the three Radarr/Sonarr migration routes. The API key
// rides in each body and is never kept anywhere but the caller's component
// state — the server does not store it either.

export function previewSource(req: ArrSourceRequest): Promise<ArrPreview> {
	return api<ArrPreview>("/library/imports/sources/preview", {
		method: "POST",
		body: req,
	});
}

export async function checkPaths(
	roots: ArrRootMapping[],
): Promise<ArrRootCheck[]> {
	const res = await api<{ roots: ArrRootCheck[] }>(
		"/library/imports/sources/check-paths",
		{ method: "POST", body: { roots } },
	);
	return res.roots;
}

export type ApplySourceConfigRequest = ArrSourceRequest & {
	indexers: ArrConfigSelection[];
	download_clients: ArrConfigSelection[];
};

export function applySourceConfig(
	req: ApplySourceConfigRequest,
): Promise<ApplySourceConfigResult> {
	return api<ApplySourceConfigResult>("/library/imports/sources/apply-config", {
		method: "POST",
		body: req,
	});
}

// Brand names stay literal in every language.
export function appLabel(app: ArrApp): "Radarr" | "Sonarr" {
	return app === "radarr" ? "Radarr" : "Sonarr";
}

// defaultRoots maps every root folder onto itself — the right answer whenever
// streamline sees the library at the same path the instance does. The sample
// rides along so check-paths can verify the mapping against a real file.
export function defaultRoots(p: ArrPreview): ArrRootMapping[] {
	return p.root_folders.map((r) =>
		r.sample_path
			? { from: r.path, to: r.path, sample_path: r.sample_path }
			: { from: r.path, to: r.path },
	);
}

export type ProfileChoice =
	| { kind: "create" }
	| { kind: "existing"; name: string }
	| { kind: "default" };

// A streamline profile already holding the source profile's name is usually
// the one the operator wants — and creating it would collide with that name.
export function defaultProfileChoice(t: ArrProfileTranslation): ProfileChoice {
	return t.existing ? { kind: "existing", name: t.existing } : { kind: "create" };
}

// profileMappings turns the per-profile choices into the start request's
// `profile_mappings`. A profile with no recorded choice takes its default, so
// the request always covers every source profile.
export function profileMappings(
	p: ArrPreview,
	choices: Map<number, ProfileChoice>,
): ArrProfileMapping[] {
	return p.quality_profiles.map((t) => {
		const choice = choices.get(t.id) ?? defaultProfileChoice(t);
		const base = { source_id: t.id, source_name: t.name };
		switch (choice.kind) {
			case "create":
				return {
					...base,
					target: t.name,
					create: { ...t.translation, name: t.name },
				};
			case "existing":
				return { ...base, target: choice.name };
			case "default":
				return { ...base, target: "" };
		}
	});
}

// blockingRootCheck holds the wizard on the paths step while any mapping that
// can be verified — one carrying a sample file — does not resolve here. The
// server refuses such a start anyway; this says so before the request.
export function blockingRootCheck(
	checks: ArrRootCheck[],
	roots: ArrRootMapping[],
): boolean {
	return roots.some((r) => {
		if (!r.sample_path) return false;
		const c = checks.find((c) => c.from === r.from && c.to === r.to);
		return !c?.found;
	});
}
