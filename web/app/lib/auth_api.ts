import { authFetch } from "./api";

export type AuthConfig = {
	registration_mode: "disabled" | "open" | "invite";
	providers: { name: string }[];
	read_only: boolean;
};

export type InvitePrefill = {
	email?: string;
	email_locked: boolean;
};

export function fetchAuthConfig(): Promise<AuthConfig> {
	return authFetch<AuthConfig>("/auth/config");
}

export function fetchInvitePrefill(token: string): Promise<InvitePrefill> {
	return authFetch<InvitePrefill>(`/auth/invite/${encodeURIComponent(token)}`);
}

export type LoginInput = { email: string; password: string };
export type RegisterInput = {
	email: string;
	password: string;
	confirm: string;
	display_name?: string;
	token?: string;
};

export function postLogin(body: LoginInput): Promise<null> {
	return authFetch<null>("/auth/login", { method: "POST", body });
}

export function postRegister(body: RegisterInput): Promise<null> {
	return authFetch<null>("/auth/register", { method: "POST", body });
}

// oidcStartURL builds the bootstrap redirect that the SPA navigates to in
// order to hand off to the IdP. The `next` parameter is carried through the
// transient state cookie and applied after the callback.
export function oidcStartURL(provider: string, next: string): string {
	const params = new URLSearchParams();
	if (next) params.set("next", next);
	const qs = params.toString();
	return `/auth/oidc/${encodeURIComponent(provider)}/start${qs ? "?" + qs : ""}`;
}

// oidcErrorMessage maps the error code carried in /login?error=... back to a
// human-readable line.
export function oidcErrorMessage(code: string | null): string {
	if (!code) return "";
	switch (code) {
		case "oidc_state_missing":
		case "oidc_state_mismatch":
		case "oidc_nonce_mismatch":
			return "Sign-in session expired. Please try again.";
		case "oidc_email_unverified":
			return "Your email address is not verified with the provider.";
		case "oidc_registration_disabled":
			return "Registration is disabled.";
		case "oidc_no_invite":
			return "An invite is required to register.";
		case "oidc_provider_error":
			return "The identity provider returned an error.";
		default:
			return "Sign-in failed.";
	}
}
