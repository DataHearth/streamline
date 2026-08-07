package web

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
)

var _ = Describe("retryAfterSeconds", Label("unit", "server"), func() {
	It("floors a sub-second wait at one second", func() {
		Expect(retryAfterSeconds(400 * time.Millisecond)).To(Equal("1"))
	})

	It("never renders zero, whatever it is handed", func() {
		Expect(retryAfterSeconds(0)).To(Equal("1"))
		Expect(retryAfterSeconds(-time.Hour)).To(Equal("1"))
	})

	It("rounds a partial second up", func() {
		Expect(retryAfterSeconds(2100 * time.Millisecond)).To(Equal("3"))
	})

	It("passes a whole wait through", func() {
		Expect(retryAfterSeconds(15 * time.Minute)).To(Equal("900"))
	})
})

var _ = Describe("userFacingRegisterError", Label("unit", "server"), func() {
	It("maps ErrInviteInvalid to the invite-invalid copy", func() {
		Expect(userFacingRegisterError(auth.ErrInviteInvalid)).
			To(Equal("Invite invalid or expired"))
	})

	It("maps wrapped ErrInviteInvalid", func() {
		wrapped := fmt.Errorf("register: %w", auth.ErrInviteInvalid)
		Expect(userFacingRegisterError(wrapped)).
			To(Equal("Invite invalid or expired"))
	})

	It("maps ent constraint errors to the duplicate-email copy", func() {
		cerr := &ent.ConstraintError{}
		Expect(ent.IsConstraintError(cerr)).To(BeTrue())
		Expect(userFacingRegisterError(cerr)).
			To(Equal("This email is already registered"))
	})

	It("maps any other error to the generic fallback", func() {
		Expect(userFacingRegisterError(errors.New("db blew up"))).
			To(Equal("Registration failed. Please try again."))
	})
})
