// One line of page state for the topbar to carry under the title.
//
// Below md the library pages give up their own count line — it costs 30px of a
// 774px viewport to say something the header has room for — so the page hands
// the line here and the topbar renders it. At md and up the pages keep their
// own line and this is ignored.

let line = $state("");

export const pageMeta = {
	get line(): string {
		return line;
	},
	set(v: string) {
		line = v;
	},
	clear() {
		line = "";
	},
};
