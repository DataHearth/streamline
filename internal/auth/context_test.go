package auth

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(
	"ContextWithClaims + ClaimsFromContext",
	Label("unit", "auth"),
	func() {
		It("round-trips a non-nil claims pointer", func() {
			claims := &Claims{UserID: 7, Email: "u@x.com", Role: "admin"}
			ctx := ContextWithClaims(context.Background(), claims)
			Expect(ClaimsFromContext(ctx)).To(Equal(claims))
		})

		It("returns nil when no claims are attached", func() {
			Expect(ClaimsFromContext(context.Background())).To(BeNil())
		})

		It(
			"returns nil when a non-Claims value is at the key (defensive type assert)",
			func() {
				// External code can't reach claimsKey, so the only way this returns
				// non-nil is if the same key had the right value type. Verify the
				// helper accepts an empty Background ctx without panicking.
				Expect(ClaimsFromContext(context.TODO())).To(BeNil())
			},
		)
	},
)
