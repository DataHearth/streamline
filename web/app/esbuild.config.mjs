import esbuild from "esbuild";
import sveltePlugin from "esbuild-svelte";

await esbuild.build({
	entryPoints: ["web/app/main.ts"],
	bundle: true,
	minify: true,
	format: "esm",
	outdir: "web/static/dist",
	entryNames: "spa.min",
	loader: { ".svg": "text" },
	tsconfig: "web/app/tsconfig.json",
	conditions: ["svelte", "browser", "module", "import"],
	mainFields: ["svelte", "browser", "module", "main"],
	plugins: [
		sveltePlugin({
			// Plugin-level filter runs after both compile() and compileModule(),
			// so it also catches warnings from third-party `.svelte.js` modules
			// (e.g. @tanstack/svelte-query, runed) that the compilerOptions
			// warningFilter misses.
			filterWarnings: (w) => !w.filename?.includes("node_modules"),
		}),
	],
	logLevel: "info",
});
