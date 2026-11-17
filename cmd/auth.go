package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/utils/numeric"
	"github.com/urfave/cli/v3"
)

// userListPageSize is the per-query cap db.ListUsersParams documents for its
// callers, so a full listing is assembled from several queries rather than one
// oversized one.
const userListPageSize = 100

// knownRoles feeds the operator-facing error message only. entuser.RoleValidator
// stays the authority on what the column accepts.
var knownRoles = []entuser.Role{
	entuser.RoleAdmin,
	entuser.RoleMember,
	entuser.RoleRequestOnly,
}

// withAuthDeps loads the config, opens the database, and builds the store and
// auth service, then runs fn. The list and set subcommands share this wiring.
func withAuthDeps(
	ctx context.Context,
	cmd *cli.Command,
	fn func(ctx context.Context, store db.Store, svc auth.Manager) error,
) error {
	if _, err := config.Load(cmd.String("config")); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dbClient, err := db.Open(ctx, config.Get().DatabasePath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer dbClient.Close()
	store := db.New(dbClient)
	svc, err := auth.New(store)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	return fn(ctx, store, svc)
}

// parseRole validates an operator-supplied role against the ent enum.
func parseRole(s string) (entuser.Role, error) {
	r := entuser.Role(strings.TrimSpace(s))
	if err := entuser.RoleValidator(r); err != nil {
		names := make([]string, 0, len(knownRoles))
		for _, k := range knownRoles {
			names = append(names, string(k))
		}
		return "", fmt.Errorf(
			"invalid role %q (valid: %s)",
			s,
			strings.Join(names, ", "),
		)
	}
	return r, nil
}

// resolveUser looks a user up by email, mapping a missing row to a message an
// operator can act on rather than the raw ent error.
func resolveUser(
	ctx context.Context,
	store db.Store,
	email string,
) (*ent.User, error) {
	u, err := store.FindUserByEmail(ctx, email)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("user %s not found", email)
		}
		return nil, fmt.Errorf("lookup %s: %w", email, err)
	}
	return u, nil
}

func formatUserTable(users []*ent.User) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tEMAIL\tROLE\tDISPLAY NAME\tCREATED")
	for _, u := range users {
		name := u.DisplayName
		if name == "" {
			name = "-"
		}
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			u.ID, u.Email, u.Role, name, u.CreateTime.Format(time.RFC3339))
	}
	_ = w.Flush()
	return sb.String()
}

func authUnlock(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: streamline auth unlock <email>")
	}
	email := strings.ToLower(strings.TrimSpace(cmd.Args().Get(0)))
	if email == "" {
		return errors.New("email is required")
	}
	return withAuthDeps(ctx, cmd, func(
		ctx context.Context,
		_ db.Store,
		svc auth.Manager,
	) error {
		if err := svc.Unlock(ctx, email, auth.UnlockModeCLI); err != nil {
			return fmt.Errorf("unlock %s: %w", email, err)
		}
		fmt.Fprintf(os.Stderr, "unlocked %s\n", email)
		return nil
	})
}

func authList(ctx context.Context, cmd *cli.Command) error {
	var roleFilter entuser.Role
	if r := cmd.String("role"); r != "" {
		parsed, err := parseRole(r)
		if err != nil {
			return err
		}
		roleFilter = parsed
	}
	limit := numeric.SaturateU32(cmd.Uint("limit"))

	return withAuthDeps(ctx, cmd, func(
		ctx context.Context,
		store db.Store,
		_ auth.Manager,
	) error {
		var users []*ent.User
		var offset uint32
		for {
			page := uint32(userListPageSize)
			if limit > 0 && limit-numeric.SaturateU32(len(users)) < page {
				page = limit - numeric.SaturateU32(len(users))
			}
			items, total, err := store.ListUsers(ctx, db.ListUsersParams{
				Role:   roleFilter,
				Limit:  page,
				Offset: offset,
				Sort:   db.UserSortCreated,
				Order:  db.UserOrderAsc,
			})
			if err != nil {
				return fmt.Errorf("list users: %w", err)
			}
			users = append(users, items...)
			offset += numeric.SaturateU32(len(items))
			if len(items) == 0 || len(users) >= total {
				break
			}
			if limit > 0 && numeric.SaturateU32(len(users)) >= limit {
				break
			}
		}
		fmt.Print(formatUserTable(users))
		return nil
	})
}

func authSetRole(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: streamline auth set-role <email> <role>")
	}
	email := strings.ToLower(strings.TrimSpace(cmd.Args().Get(0)))
	if email == "" {
		return errors.New("email is required")
	}
	role, err := parseRole(cmd.Args().Get(1))
	if err != nil {
		return err
	}

	return withAuthDeps(ctx, cmd, func(
		ctx context.Context,
		store db.Store,
		svc auth.Manager,
	) error {
		u, err := resolveUser(ctx, store, email)
		if err != nil {
			return err
		}
		if u.Role == role {
			fmt.Fprintf(os.Stderr, "%s already has role %s\n", email, role)
			return nil
		}
		target := string(role)
		err = svc.UpdateUser(ctx, u.ID, auth.UserPatch{Role: &target})
		if err != nil {
			if errors.Is(err, auth.ErrLastAdmin) {
				return fmt.Errorf("refusing to demote %s: %w", email, err)
			}
			return fmt.Errorf("set role of %s: %w", email, err)
		}
		fmt.Fprintf(os.Stderr, "changed role of %s: %s -> %s\n", email, u.Role, role)
		return nil
	})
}

func authSetPassword(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: streamline auth set-password <email>")
	}
	email := strings.ToLower(strings.TrimSpace(cmd.Args().Get(0)))
	if email == "" {
		return errors.New("email is required")
	}
	explicit := cmd.String("password")
	generate := cmd.Bool("generate")
	if (explicit != "") == generate {
		return errors.New("exactly one of --password or --generate is required")
	}

	return withAuthDeps(ctx, cmd, func(
		ctx context.Context,
		store db.Store,
		svc auth.Manager,
	) error {
		u, err := resolveUser(ctx, store, email)
		if err != nil {
			return err
		}
		password := explicit
		if generate {
			password, err = auth.GeneratePassword()
			if err != nil {
				return fmt.Errorf("generate password: %w", err)
			}
		}
		// AdminResetPassword also revokes every session the user holds, which
		// is what an operator resetting a compromised account wants.
		if err := svc.AdminResetPassword(ctx, u.ID, password); err != nil {
			if errors.Is(err, auth.ErrPasswordWeak) {
				return errors.New("password rejected: must be at least 8 characters")
			}
			return fmt.Errorf("set password of %s: %w", email, err)
		}
		if generate {
			// stdout, never slog: log records also reach the OTLP pipeline.
			fmt.Printf("new password for %s: %s\n", email, password)
			fmt.Fprintln(os.Stderr, "shown once; copy it now")
			return nil
		}
		fmt.Fprintf(os.Stderr, "password updated for %s\n", email)
		return nil
	})
}
