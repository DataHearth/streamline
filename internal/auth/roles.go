package auth

// roleRank orders roles by privilege, so an OIDC claim mapping can pick the
// highest-privilege match and callers can compare a claim against a minimum.
var roleRank = map[string]int{"request_only": 1, "member": 2, "admin": 3}

// RoleAtLeast reports whether role carries at least the privilege of min.
// Fails closed: an unrecognised role or minimum satisfies nothing.
func RoleAtLeast(role, min string) bool {
	r, ok := roleRank[role]
	m, okMin := roleRank[min]
	return ok && okMin && r >= m
}
