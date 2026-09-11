import type { FormApi, SvelteFormApi } from "@tanstack/svelte-form";

// createForm's real return type is FormApi<...> & SvelteFormApi<...> — an
// intersection the package builds internally (as `SvelteFormExtendedApi` in
// createForm.svelte.d.ts) but does not export from its root, so `.Field` /
// `.useStore` / `.Subscribe` live on the SvelteFormApi half and have to be
// composed back in here.
//
// Both halves take 12 type parameters: TFormData plus eleven invariant
// ("in out") validator slots (TOnMount..TOnServer, TSubmitMeta). Settings
// form components only ever call form.Field / form.state / form.setFieldValue
// — none of them run or read a validator — but the parent route constructs
// the form with a concrete valibot schema for one of those slots, and
// invariance means a component-declared `undefined` there rejects that
// concrete schema outright. `any` is the only slot value that is (variance-
// wise) compatible with whatever the parent passed, so it stands in for all
// eleven here rather than in each of the six form components individually.
export type AppForm<TFormData> = FormApi<
	TFormData,
	any,
	any,
	any,
	any,
	any,
	any,
	any,
	any,
	any,
	any,
	any
> &
	SvelteFormApi<
		TFormData,
		any,
		any,
		any,
		any,
		any,
		any,
		any,
		any,
		any,
		any,
		any
	>;

// The one text-input class for every settings control, whether the page uses
// TanStack forms or saves per control. Read-only styling is part of it rather
// than something each page remembers: a locked instance still has to be
// readable and selectable, so the field keeps its colours, but it must stop
// inviting the edit it will not accept — no I-beam, and no focus ring, which
// on a field that cannot change reads as "type here".
export const INPUT_CLASS =
	"w-full rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg " +
	"placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 " +
	"focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70 " +
	"read-only:focus-visible:ring-0 read-only:focus-visible:outline-none " +
	"read-only:focus:border-border";
