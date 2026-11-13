import type { AppAccess, AppAccessCreated } from "./types";

// The plaintext secret is for the reveal panel alone; the query cache keeps
// the card's data without it.
export const withoutSecret = ({ enabled, username, created_at, last_used_at, last_client }: AppAccessCreated): AppAccess => ({
	enabled,
	username,
	created_at,
	last_used_at,
	last_client,
});
