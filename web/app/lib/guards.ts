import { goto } from "@roxi/routify";
import { get } from "svelte/store";
import { auth } from "./auth.svelte";

export function requireAdmin() {
	if (auth.loading) return;
	if (auth.user?.role !== "admin") {
		get(goto)("/forbidden");
	}
}
