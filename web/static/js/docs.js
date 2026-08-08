import { createApiReference } from "@scalar/api-reference";
import "@scalar/api-reference/style.css";

// Scalar's default theme declares Inter and JetBrains Mono over fourteen woff2
// subsets served from https://fonts.scalar.com. font-src is 'self' (see
// internal/server/middleware/security_headers.go), so on a self-hosted install
// those would be fourteen blocked requests and fourteen console errors on every
// /api/docs load — enough noise to bury the next genuine CSP violation — and
// allowlisting the CDN instead would announce every visit to the API docs to a
// third party.
//
// withDefaultFonts drops Scalar's @font-face block entirely and customCss
// re-declares the same two families over the copies web/static/fonts already
// ships for the SPA, so the page keeps its intended typefaces same-origin.
// These rules duplicate web/static/css/input.css because the docs page loads
// only docs.min.css, and esbuild cannot resolve a root-absolute url() out of a
// bundled stylesheet without an --external flag. web/static/fonts is the single
// source of truth for the files themselves; internal/server/web/spa_test.go
// fails if a face named here stops resolving there.
const selfHostedFonts = `
@font-face {
	font-family: "Inter";
	src: url("/static/fonts/Inter-Regular.woff2") format("woff2");
	font-weight: 400;
	font-style: normal;
	font-display: swap;
}
@font-face {
	font-family: "Inter";
	src: url("/static/fonts/Inter-Medium.woff2") format("woff2");
	font-weight: 500;
	font-style: normal;
	font-display: swap;
}
@font-face {
	font-family: "Inter";
	src: url("/static/fonts/Inter-SemiBold.woff2") format("woff2");
	font-weight: 600;
	font-style: normal;
	font-display: swap;
}
@font-face {
	font-family: "Inter";
	src: url("/static/fonts/Inter-Bold.woff2") format("woff2");
	font-weight: 700;
	font-style: normal;
	font-display: swap;
}
@font-face {
	font-family: "JetBrains Mono";
	src: url("/static/fonts/JetBrainsMono-Regular.woff2") format("woff2");
	font-weight: 400 500;
	font-style: normal;
	font-display: swap;
}
`;

createApiReference("#app", {
	url: "/api/v1/openapi.yaml",
	darkMode: true,
	favicon: "/static/images/favicon-512.png",
	showOperationId: true,
	telemetry: false,
	withDefaultFonts: false,
	customCss: selfHostedFonts,
});
