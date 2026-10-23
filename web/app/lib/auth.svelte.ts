import { api } from "./api";
import type { User } from "./types";

class AuthStore {
	user = $state<User | null>(null);
	loading = $state(true);

	get isAdmin() {
		return this.user?.role === "admin";
	}

	// admin + member add to the library directly; request_only may only request.
	get canAddDirectly() {
		return this.user?.role === "admin" || this.user?.role === "member";
	}

	async hydrate() {
		this.loading = true;
		try {
			this.user = await api<User>("/auth/me");
		} finally {
			this.loading = false;
		}
	}
}

export const auth = new AuthStore();
