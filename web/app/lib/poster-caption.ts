// The text classes of a poster's caption overlay, shared by every card that
// draws one (PosterCard, ShelfCard, the "More like this" row) so they clamp
// alike. At rest the title takes two lines and the line under it one. Hovering
// the card, or moving keyboard focus onto it, lifts both clamps: the overlay
// is anchored to the poster's bottom edge, so the whole text shows by growing
// upward over the art and nothing around the card moves. A touch screen has no
// hover and keeps the two-line title.
const open =
	"group-hover:line-clamp-none group-focus-visible:line-clamp-none group-has-[:focus-visible]:line-clamp-none";

export const captionTitle = `line-clamp-2 break-words ${open}`;
export const captionSubtitle = `line-clamp-1 break-words ${open}`;
