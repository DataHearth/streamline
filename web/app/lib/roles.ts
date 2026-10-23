// Display names for user roles. The role value itself ("request_only") is an
// API enum and never shown as is.

import { m as i18n } from "./paraglide/messages.js";
import type { UserRole } from "./types";

export function roleLabel(role: UserRole): string {
	switch (role) {
		case "admin":
			return i18n.common_admin();
		case "member":
			return i18n.role_member();
		case "request_only":
			return i18n.role_request_only();
		default: {
			const unknown: never = role;
			return unknown;
		}
	}
}
