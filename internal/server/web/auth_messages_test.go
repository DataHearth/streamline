package web

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
)

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
