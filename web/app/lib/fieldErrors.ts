import type { AnyFieldApi } from "@tanstack/form-core";

// Validation errors arrive as valibot issue objects, but a raw string is also a
// legal error value — normalise both into displayable text.
export function fieldErrorMessages(field: AnyFieldApi): string[] {
	return field.state.meta.errors.map((e: unknown) => {
		if (e && typeof e === "object" && "message" in e)
			return String((e as { message: unknown }).message);
		return String(e);
	});
}
