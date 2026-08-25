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
