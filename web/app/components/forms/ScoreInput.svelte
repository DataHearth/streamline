<script lang="ts">
	type Props = {
		value: number;
		onChange: (n: number) => void;
		readonly?: boolean;
		ariaLabel?: string;
		class?: string;
	};

	let {
		value,
		onChange,
		readonly = false,
		ariaLabel,
		class: klass = "",
	}: Props = $props();

	// The typed string is kept apart from the committed number. A signed score is
	// reached one keystroke at a time, and "-" — the first of them — parses to
	// NaN: coercing it to 0 and writing that back re-rendered the input and ate
	// the minus, so "-1000" could only ever be pasted, never typed.
	let text = $state(String(value));
	let committed = $state(value);

	// Only an outside write (a preset, an edit dialog reopening) re-renders the
	// field; reading `text` here would let the incomplete-string case above
	// clobber itself.
	$effect(() => {
		if (value !== committed) {
			committed = value;
			text = String(value);
		}
	});

	function parse(raw: string): number | null {
		const t = raw.trim();
		if (t === "" || t === "-" || t === "+") return null;
		const n = Number(t);
		return Number.isFinite(n) ? Math.trunc(n) : null;
	}

	function onInput(e: Event) {
		text = (e.currentTarget as HTMLInputElement).value;
		const n = parse(text);
		if (n === null || n === committed) return;
		committed = n;
		onChange(n);
	}

	// Leaving the field is what settles an unfinished number: an empty box, or a
	// lone sign, is 0.
	function onBlur() {
		const n = parse(text) ?? 0;
		text = String(n);
		if (n !== committed) {
			committed = n;
			onChange(n);
		}
	}
</script>

<!-- type="text", not "number": a number input reports "" for anything it cannot
     parse, so the "-" on its way to "-1000" never reaches the handler at all.
     inputmode keeps the numeric keypad on a phone. -->
<input
	type="text"
	inputmode="numeric"
	spellcheck="false"
	autocomplete="off"
	{readonly}
	aria-label={ariaLabel}
	value={text}
	oninput={onInput}
	onblur={onBlur}
	class={klass}
/>
