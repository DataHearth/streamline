import { createApiReference } from "@scalar/api-reference";
import "@scalar/api-reference/style.css";

createApiReference("#app", {
	url: "/api/v1/openapi.yaml",
	darkMode: true,
	favicon: "/static/images/favicon-512.png",
	showOperationId: true,
	telemetry: false,
});
