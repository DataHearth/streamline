// Split a whole translated sentence around one slot, so the slot can be
// rendered with its own markup (a highlighted status word) without cutting the
// sentence into fragments that each language would have to order the same way.
//
//   const [pre, post] = around((status) => i18n.some_message({ status }));
//   {pre}<b>{i18n.lc_wanted()}</b>{post}

const MARK = "\u0000";

export function around(render: (slot: string) => string): [string, string] {
	const s = render(MARK);
	const i = s.indexOf(MARK);
	return i < 0 ? [s, ""] : [s.slice(0, i), s.slice(i + MARK.length)];
}
