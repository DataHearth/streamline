export type CardigannYamlEntry = {
	name: string;
	enabled: boolean;
	priority: number;
	private: boolean;
	definition: string;
	commit: string;
	site?: string;
	proxy?: string;
	settings: Record<string, string>;
};

// A JSON string is a YAML double-quoted scalar, so every value stays a
// string whatever it reads as ("true", "25", a key holding ":" or "#").
// JSON leaves U+0085, U+2028 and U+2029 bare, and YAML folds them into a
// space, so they are escaped too.
const str = (v: string) =>
	JSON.stringify(v).replace(
		/[\u0085\u2028\u2029]/g,
		(c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`,
	);
const key = (k: string) => (/^[A-Za-z0-9_-]+$/.test(k) ? k : str(k));

// indexerYaml is one item of the config file's `indexers:` list, for a
// read-only instance whose config is edited by hand; it goes under that key.
export function indexerYaml(e: CardigannYamlEntry): string {
	const lines = [
		`  - name: ${str(e.name)}`,
		"    protocol: cardigann",
		`    enabled: ${e.enabled}`,
		`    priority: ${e.priority}`,
		`    private: ${e.private}`,
		"    cardigann:",
		`      definition: ${str(e.definition)}`,
		`      commit: ${str(e.commit)}`,
	];
	if (e.site) lines.push(`      site: ${str(e.site)}`);
	if (e.proxy) lines.push(`      proxy: ${str(e.proxy)}`);
	const settings = Object.entries(e.settings).sort(([a], [b]) => a.localeCompare(b));
	if (settings.length > 0) {
		lines.push("      settings:");
		for (const [n, v] of settings) lines.push(`        ${key(n)}: ${str(v)}`);
	}
	return lines.join("\n");
}
