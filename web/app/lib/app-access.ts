import type { AppAccess, AppAccessCreated, AppAccessKind } from "./types";

export const APP_ACCESS_PATH: Record<AppAccessKind, string> = {
	subsonic: "/account/subsonic-password",
	opds: "/account/opds-token",
};

export const appAccessKey = (kind: AppAccessKind) => ["account", kind];

// The plaintext secret is for the reveal panel alone; the query cache keeps
// the card's data without it.
export const withoutSecret = ({ enabled, username, created_at, last_used_at, last_client }: AppAccessCreated): AppAccess => ({
	enabled,
	username,
	created_at,
	last_used_at,
	last_client,
});
