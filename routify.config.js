// Routify v3 build-time config. Routify auto-detects Svelte 5 via
// node_modules/svelte version (see RoutifyBuildtime.js: svelteApi
// resolves to 5 for Svelte 5.x). The CLI scans routesDir and writes
// a routes manifest under routifyDir for the runtime to import.
export default {
	routesDir: {
		default: "web/app/routes",
	},
	routifyDir: "web/app/.routify",
	rootComponent: "web/app/App.svelte",
	mainEntryPoint: "web/app/main.js",
	extensions: [".svelte"],
};
