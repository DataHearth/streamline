<script lang="ts">
	import { cn } from "../../lib/cn";

	let {
		w = "100%",
		h = 10,
		round = "sm",
		class: klass = "",
	}: {
		w?: string;
		h?: number;
		round?: "sm" | "md" | "full";
		class?: string;
	} = $props();
</script>

<!--
  One placeholder bar. The fill is the ProgressBar track's — the app already
  reads that value as "something is happening here" — and the motion is the
  pulse the release table used, so nothing new enters the vocabulary.
-->
<div
	aria-hidden="true"
	class={cn(
		"shrink-0 animate-pulse bg-white/[0.06] motion-reduce:animate-none",
		round === "full" ? "rounded-full" : round === "md" ? "rounded-md" : "rounded",
		klass,
	)}
	style="width:{w};height:{h}px"
></div>
