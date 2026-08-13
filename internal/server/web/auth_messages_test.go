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

	It("gives a taken email the same answer as any other failure", func() {
		cerr := &ent.ConstraintError{}
		Expect(ent.IsConstraintError(cerr)).To(BeTrue())
		Expect(userFacingRegisterError(cerr)).
			To(Equal(userFacingRegisterError(errors.New("db blew up"))))
	})

	It("maps any other error to the neutral fallback", func() {
		Expect(userFacingRegisterError(errors.New("db blew up"))).
			To(Equal(
				"Registration failed. If you already have an account, sign in instead.",
			))
	})

	It("never says whether the email is registered", func() {
		for _, err := range []error{
			&ent.ConstraintError{},
			fmt.Errorf("create user: %w", &ent.ConstraintError{}),
			errors.New("db blew up"),
		} {
			msg := userFacingRegisterError(err)
			Expect(msg).NotTo(ContainSubstring("already registered"))
			Expect(msg).NotTo(ContainSubstring("exists"))
		}
	})
})
