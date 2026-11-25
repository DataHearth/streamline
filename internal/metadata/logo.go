package metadata

import (
	"context"
	"slices"
	"strings"
)

type logosKey struct{}

// WithLogos asks GetMovie for the title's logos too. TMDB sends its image
// lists only on request, and the lookups and matchers that share the call
// never draw one.
func WithLogos(ctx context.Context) context.Context {
	return context.WithValue(ctx, logosKey{}, true)
}

func wantsLogos(ctx context.Context) bool {
	want, _ := ctx.Value(logosKey{}).(bool)
	return want
}

// logoImage is one provider's logo candidate: lang is the provider's own
// language code, empty for a textless logo, and score its ranking.
type logoImage struct {
	ref   string
	lang  string
	score float64
}

// rankLogos lists the logos a detail hero may show, best first: those in the
// metadata language, else the English ones, else those tagged with no
// language, each group ordered by score with ties kept in the provider's
// order. Another language's logo is never shown. lang and english are codes in
// the provider's own standard.
func rankLogos(logos []logoImage, lang, english string) []string {
	rank := func(code string) int {
		switch {
		case code != "" && strings.EqualFold(code, lang):
			return 0
		case strings.EqualFold(code, english):
			return 1
		case code == "":
			return 2
		}
		return 3
	}
	best := 3
	var kept []logoImage
	for _, l := range logos {
		r := rank(l.lang)
		if l.ref == "" || r > best || r == 3 {
			continue
		}
		if r < best {
			best, kept = r, kept[:0]
		}
		kept = append(kept, l)
	}
	slices.SortStableFunc(kept, func(a, b logoImage) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return 0
	})
	refs := make([]string, len(kept))
	for i, l := range kept {
		refs[i] = l.ref
	}
	return refs
}
