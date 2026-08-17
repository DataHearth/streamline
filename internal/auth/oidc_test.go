package auth

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"golang.org/x/tools/go/packages"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("oidcClaimRoles", Label("unit", "auth"), func() {
	pc := config.OIDCConfig{
		Name:        "kc",
		RoleClaim:   "groups",
		RoleMapping: map[string]string{"admins": "admin", "staff": "member"},
	}

	It("returns every matching group's role, uncapped", func() {
		Expect(oidcClaimRoles(pc,
			map[string]any{"groups": []any{"staff", "admins"}})).
			To(ConsistOf("member", "admin"))
	})

	It("accepts a single string claim", func() {
		Expect(oidcClaimRoles(pc, map[string]any{"groups": "staff"})).
			To(ConsistOf("member"))
	})

	It("returns nothing when no group maps", func() {
		Expect(oidcClaimRoles(pc, map[string]any{"groups": []any{"randos"}})).
			To(BeEmpty())
	})

	It("returns nothing when the provider configures no mapping", func() {
		Expect(oidcClaimRoles(config.OIDCConfig{Name: "kc"},
			map[string]any{"groups": "admins"})).To(BeEmpty())
	})

	// The ceiling is capOIDCRole's job, not this function's: an admin group
	// still comes back here and is dropped downstream. Splitting them is what
	// leaves exactly one place that can decide a role.
	It("still reports an admin group for a provider without allow_admin", func() {
		Expect(oidcClaimRoles(pc, map[string]any{"groups": []any{"admins"}})).
			To(ConsistOf("admin"))
	})

	It("resolves a dotted role_claim through nested claims", func() {
		kc := config.OIDCConfig{
			Name:        "kc",
			RoleClaim:   "realm_access.roles",
			RoleMapping: map[string]string{"admins": "admin"},
		}
		Expect(oidcClaimRoles(kc, map[string]any{
			"realm_access": map[string]any{"roles": []any{"admins"}},
		})).To(ConsistOf("admin"))
	})

	It("prefers a claim literally named with the dot over the path", func() {
		kc := config.OIDCConfig{
			Name:        "kc",
			RoleClaim:   "realm_access.roles",
			RoleMapping: map[string]string{"flat": "member", "nested": "admin"},
		}
		Expect(oidcClaimRoles(kc, map[string]any{
			"realm_access.roles": []any{"flat"},
			"realm_access":       map[string]any{"roles": []any{"nested"}},
		})).To(ConsistOf("member"))
	})

	It("returns nothing when a dotted path runs off a non-object", func() {
		kc := config.OIDCConfig{
			Name:        "kc",
			RoleClaim:   "realm_access.roles",
			RoleMapping: map[string]string{"admins": "admin"},
		}
		Expect(oidcClaimRoles(kc,
			map[string]any{"realm_access": "not-an-object"})).To(BeEmpty())
	})
})

// roleWrite is one place in package auth that names a user's role field.
type roleWrite struct {
	fn   string
	pos  string
	what string
	// ok reports that the compiler, not the spelling, vouches for the value:
	// either a capped role.Value on its way out, or a role read back off a
	// user row. Everything else is for the allow-list to explain.
	ok bool
}

func (w roleWrite) String() string {
	verdict := "unvouched"
	if w.ok {
		verdict = "vouched"
	}
	return fmt.Sprintf("%s: %s [%s] (in %s)", w.pos, w.what, verdict, w.fn)
}

func roleWritesIn(writes []roleWrite, fn string) int {
	n := 0
	for _, w := range writes {
		if w.fn == fn {
			n++
		}
	}
	return n
}

// roleWriters are the functions that decide a user's role without asking
// role.Federated, each because no OIDC login reaches it. Adding a name here
// asserts that, and the reachability spec below is what checks the assertion
// rather than taking it — round 5's allow-list carried the same claim in prose
// and an injected `s.UpdateUser(ctx, u.ID, UserPatch{Role: &esc})` inside
// LoginOIDC escalated to admin with every spec green.
// roleWriters is now read by the REACHABILITY spec alone, and it matters more
// there than it used to. The shape walker stopped flagging what these functions
// do internally, because db's params take a role.Value and the compiler vouches
// every one of them — but each still calls a non-OIDC constructor in
// internal/role, and Go cannot express "only this package may call that". So
// the residue the type change leaves is exactly "code on an OIDC login's path
// calls role.Operator/Seed/Invited/SelfRegistered instead of role.Federated",
// and keeping an OIDC login out of these functions is what closes it.
var roleWriters = map[string]string{
	"BootstrapSeedAdmin": "seeds the admin against an empty database, before any login exists",
	"RegisterOpen":       "local registration; the role is auth.oidc_default_role's peer, not a claim",
	"Register":           "local registration through the webui",
	"RegisterWithInvite": "local registration; the invite's role, reached only with a password",
	"CreateUserDirect":   "admin-created account, over an authenticated admin session",
	"UpdateUser":         "admin role change, over an authenticated admin session",
	"CreateInvite":       "the invite's own role, over an authenticated admin session; it reaches a user through RegisterWithInvite or, over SSO, through role.Federated",
}

// roleCarriers name a role field without deciding one a login can raise. They
// are exempt from the shape rule and, unlike roleWriters, are allowed to sit on
// an OIDC login's path.
//
// That makes this the one allow-list nothing checks: a roleWriters entry is
// held to "no OIDC login reaches it" by the reachability spec, while a
// roleCarriers entry is a bare assertion. Adding a name here is the cheapest
// way to reopen this hole — reach for roleWriters first and let the
// reachability spec argue with you.
//
// issueToken used to be the second entry, which exempted its whole body and
// left `Claims{Role: "admin"}` — a JWT forgery an OIDC login reaches — outside
// every spec. It is now held to a shape instead (see rowRole), so the
// exemption covers the mirror rather than the function.
var roleCarriers = map[string]string{
	"ListUsers": "Role is a list filter, not a value written to a row",
}

const (
	authPkgPath = "github.com/datahearth/streamline/internal/auth"
	rolePkgPath = "github.com/datahearth/streamline/internal/role"
	entPkgPath  = "github.com/datahearth/streamline/ent"
)

// The guard type-checks package auth instead of parsing it, because a name is
// not a type. Round 6's walker trusted any expression spelled
// `<x>.EntPtr()` whatever `<x>` turned out to be, so
//
//	type shadowRole struct{ r entuser.Role }
//	func (f shadowRole) EntRolePtr() *entuser.Role { return &f.r }
//	params.Role = shadowRole{r: entuser.RoleAdmin}.EntPtr()
//
// dropped into syncOIDCProfile — not allow-listed, and on an OIDC login's path
// — landed an admin row, an admin JWT and an admin session with all thirteen
// specs green. A type checker resolves that receiver to auth.shadowRole, and
// the spelling stops being worth anything on its own.
var (
	loadOnce sync.Once
	loadPkg  *packages.Package
	loadErr  error
)

// loadAuthPackage resolves package auth's imports to type information. That is
// the only thing go/packages is asked for: the files themselves are re-parsed
// and re-checked below, so an injected escalation goes through exactly the code
// that checks the real tree. `go list` dominates this suite's runtime, so it
// runs once per process.
func loadAuthPackage() (*packages.Package, error) {
	loadOnce.Do(func() {
		pkgs, err := packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedCompiledGoFiles |
				packages.NeedImports | packages.NeedDeps |
				packages.NeedTypes | packages.NeedTypesSizes,
		}, ".")
		if err != nil {
			loadErr = err
			return
		}
		for _, p := range pkgs {
			if p.PkgPath == authPkgPath {
				loadPkg = p
			}
		}
		if loadPkg == nil {
			loadErr = errors.New("package auth did not load")
		}
	})
	return loadPkg, loadErr
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// patch splices source into package auth before it is type-checked. Injections
// therefore land in the function they would really have to live in, against the
// package's real types, and are judged by the production walker rather than by
// a hand-built lookalike.
type patch struct {
	anchor  string // a line of package auth; stmts are spliced in next to it
	stmts   string
	before  bool   // splice above the anchor rather than below
	decls   string // appended to the file the anchor was found in
	imports string // a whole import declaration, spliced after the package clause
}

// authScan is package auth as the compiler sees it.
type authScan struct {
	fset  *token.FileSet
	files []*ast.File
	info  *types.Info
	pkg   *types.Package
	// roleType and userType are the instances the checker itself was handed.
	// types.Identical compares named types by object identity, so a second
	// incarnation of the same declaration would answer false to everything.
	roleType types.Type
	userType types.Type
	errs     []error
}

// scanAuth parses and type-checks every non-test file of package auth, with
// patches applied.
//
// Subdirectories are not in the pattern, so role is not scanned. It is the
// trusted core rather than an oversight: it is the one package allowed to
// decide a role, it is small enough to read in full, and its own specs cover
// the ceiling. Scanning it would only report Cap's own Role{role: best}.
func scanAuth(patches ...patch) *authScan {
	GinkgoHelper()
	pkg, err := loadAuthPackage()
	Expect(err).NotTo(HaveOccurred())

	imports := map[string]*types.Package{}
	for path, imp := range pkg.Imports {
		if imp.Types != nil {
			imports[path] = imp.Types
		}
	}
	Expect(imports).To(HaveKey(rolePkgPath))
	Expect(imports).To(HaveKey(entPkgPath))
	roleObj := imports[rolePkgPath].Scope().Lookup("Value")
	userObj := imports[entPkgPath].Scope().Lookup("User")
	Expect(roleObj).NotTo(BeNil())
	Expect(userObj).NotTo(BeNil())

	// A rename or a move must not quietly leave this scanning nothing.
	names := make([]string, 0, len(pkg.CompiledGoFiles))
	for _, path := range pkg.CompiledGoFiles {
		names = append(names, filepath.Base(path))
	}
	Expect(names).To(ContainElement("oidc.go"))
	Expect(len(names)).To(BeNumerically(">=", 10))

	sources := make([]string, len(pkg.CompiledGoFiles))
	for i, path := range pkg.CompiledGoFiles {
		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		sources[i] = string(raw)
	}
	for _, p := range patches {
		hits := 0
		for i, src := range sources {
			n := strings.Count(src, p.anchor)
			hits += n
			if n == 0 {
				continue
			}
			spliced := p.anchor + "\n" + p.stmts
			if p.before {
				spliced = p.stmts + "\n" + p.anchor
			}
			src = strings.Replace(src, p.anchor, spliced, 1)
			if p.decls != "" {
				src += "\n" + p.decls + "\n"
			}
			if p.imports != "" {
				src = strings.Replace(src, "package auth\n",
					"package auth\n\n"+p.imports+"\n", 1)
			}
			sources[i] = src
		}
		// An anchor that stopped matching injects nothing, and a spec that
		// injects nothing passes on an escalation it never made.
		Expect(hits).To(Equal(1), "anchor %q", p.anchor)
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for i, src := range sources {
		file, err := parser.ParseFile(fset, pkg.CompiledGoFiles[i], src, 0)
		Expect(err).NotTo(HaveOccurred())
		files = append(files, file)
	}

	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var errs []error
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if p, ok := imports[path]; ok {
				return p, nil
			}
			return nil, fmt.Errorf("%q is not an import of package auth", path)
		}),
		Sizes: pkg.TypesSizes,
		// Collecting rather than stopping: an injection that names a params
		// type nobody has invented yet is a type error on purpose, and the
		// walker still has to report the write.
		Error: func(err error) { errs = append(errs, err) },
	}
	checked, _ := conf.Check(authPkgPath, fset, files, info)
	return &authScan{
		fset:     fset,
		files:    files,
		info:     info,
		pkg:      checked,
		roleType: roleObj.Type(),
		userType: userObj.Type(),
		errs:     errs,
	}
}

// roleWrites reports each place package auth names a Role field: a composite
// literal's `Role:` key, an assignment to a `.Role` field, an ent builder's
// SetRole/SetNillableRole. Each carries the verdict on its value, which the
// allow-list then either needs or does not.
//
// No filter on the literal's type, deliberately. Round 5's version only looked
// inside types whose name ended in UserParams, so `UserPatch{Role: &esc}` —
// declared in this very package — was invisible to it. Every type is cheaper to
// scan than to enumerate correctly.
func (a *authScan) roleWrites() []roleWrite {
	var writes []roleWrite
	for _, file := range a.files {
		for _, decl := range file.Decls {
			fnName := "<package level>"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fnName = fn.Name.Name
			}
			add := func(pos token.Pos, what string, ok bool) {
				writes = append(writes, roleWrite{
					fn:   fnName,
					pos:  a.fset.Position(pos).String(),
					what: what,
					ok:   ok,
				})
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CompositeLit:
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok || key.Name != "Role" {
							continue
						}
						add(kv.Pos(), "a Role: key in a composite literal",
							a.vouched(kv.Value))
					}
				case *ast.AssignStmt:
					for i, lhs := range node.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "Role" {
							continue
						}
						add(lhs.Pos(), "an assignment to a .Role field",
							i < len(node.Rhs) && a.vouched(node.Rhs[i]))
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if ok && (sel.Sel.Name == "SetRole" ||
						sel.Sel.Name == "SetNillableRole") {
						add(node.Pos(), "an ent builder's "+sel.Sel.Name, false)
					}
				}
				return true
			})
		}
	}
	return writes
}

// vouched reports whether the compiler backs e as a role package auth did not
// choose for itself.
func (a *authScan) vouched(e ast.Expr) bool {
	return a.cappedRole(e) || a.rowRole(e)
}

// cappedRole reports whether e is <x>.Ent() or <x>.EntPtr() called on
// an role.Value — the two exits from the ceiling, one per field shape.
//
// What is checked is the receiver's type, not its spelling. role.Value's
// field is unexported and its package exports no constructor but Cap, so —
// short of the unsafe write the uncovered table below leaves open — every
// expression of that type is either Cap's output or the zero value, and the
// zero value is the "no role decided" that write paths skip and RBAC ranks
// below everything.
//
// Round 6 wrote that same sentence against a walker that never resolved the
// receiver, so it was backed by nothing: a shadowRole declared in this package
// spelled EntRolePtr() and escalated an OIDC login to admin with every spec
// green. The claim is worth what the compiler backs, and it is the type
// identity here that makes the compiler back it.
// cappedRole reports whether e already IS a role.Value — the type db's params
// now take, and one only internal/role can fill in.
//
// It asks the type checker rather than matching a spelling. Earlier rounds
// looked for a call to `.EntRole()`/`.EntRolePtr()`, which was a name and not a
// type: a five-line shadow struct carrying a method of that name satisfied it
// and landed an admin row. Asking what the expression IS cannot be spelled
// around, because the only way to obtain a non-zero role.Value is a constructor
// in that package.
//
// The compiler now rejects a raw entuser.Role at these sites on its own, so
// this is a backstop for what remains string-typed — the Claims role in the JWT
// — rather than the thing holding the ceiling up.
func (a *authScan) cappedRole(e ast.Expr) bool {
	tv, ok := a.info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	return types.Identical(deref(tv.Type), a.roleType)
}

// rowRole reports whether e is string(<x>.Role) for an *ent.User: a role read
// back off a row, which mirrors a decision instead of making one. Whatever put
// it on the row passed the ceiling.
//
// It exists so issueToken can mirror the row into the JWT it signs without the
// whole function being exempt. What it proves is that the value came off a user
// row, not that it came off *this* login's row.
func (a *authScan) rowRole(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	conv, ok := call.Fun.(*ast.Ident)
	if !ok || conv.Name != "string" {
		return false
	}
	// A conversion, not a call of some function that happens to be named
	// string — only the former has a type on the left of the parenthesis.
	if tv, ok := a.info.Types[call.Fun]; !ok || !tv.IsType() {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Role" {
		return false
	}
	row := a.info.TypeOf(sel.X)
	return row != nil && types.Identical(deref(row), a.userType)
}

func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// callGraph maps each function of package auth to the package functions it
// names. Resolved by the type checker rather than matched by name, which is
// what closes round 6's other gap: `up := s.UpdateUser` followed by `up(...)`
// called an identifier the syntax walker had never heard of, and reached
// UpdateUser without an edge to it.
//
// Every reference counts, not only the ones in call position, because taking a
// method's value is how a call gets made without spelling one.
//
// Foreign receivers drop out on their own. `s.db.UpdateUser` and
// `tx.CreateUser` resolve into package db, where the syntax walker needed a
// hand-maintained list of receiver names to tell them from the service's own
// methods — and a receiver spelled some third way would have been read as one.
func (a *authScan) callGraph() map[string]map[string]bool {
	graph := map[string]map[string]bool{}
	for _, file := range a.files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			callees, seen := graph[fn.Name.Name]
			if !seen {
				callees = map[string]bool{}
				graph[fn.Name.Name] = callees
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				target, ok := a.info.Uses[id].(*types.Func)
				if ok && target.Pkg() == a.pkg {
					callees[target.Name()] = true
				}
				return true
			})
		}
	}
	return graph
}

// reachableFrom walks graph breadth-first and returns every function root can
// reach.
func reachableFrom(graph map[string]map[string]bool, root string) map[string]bool {
	seen := map[string]bool{}
	queue := []string{root}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		if seen[fn] {
			continue
		}
		seen[fn] = true
		queue = append(queue, slices.Collect(maps.Keys(graph[fn]))...)
	}
	return seen
}

const (
	// syncOIDCProfile is on an OIDC login's path and is not allow-listed —
	// the function round 6's bypass was measured in.
	syncOIDCProfileAnchor = "\tparams := db.UpdateUserParams{}\n\tdirty := false"
	loginOIDCAnchor       = "\temail = strings.ToLower(strings.TrimSpace(email))"
)

// The ceiling is unskippable because role.Value has no exported way to hold
// a role role.Federated did not put there — that part the compiler enforces, in
// this package and every other, and no test can be blind to it. What the
// compiler does not cover is a raw entuser.Role written straight into a db
// params struct, which is what these specs scan for, and a login reaching one
// of the functions allowed to write one, which is what the reachability spec
// checks.
//
// Package-wide rather than oidc.go-wide: a helper one file over is as reachable
// from LoginOIDC as one in the same file, and file scope is a boundary an
// attacker picks, not one the compiler enforces.
//
// What is still outside all of it is enumerated by the "leave uncovered" table
// below, which injects each shape and watches nothing happen. Prose alone is
// how round 6 came to claim a receiver the walker never resolved.
var _ = Describe("OIDC role writes", Label("unit", "auth"), func() {
	// A type error anywhere in the package leaves the checker guessing, and a
	// guess resolves an EntRolePtr() receiver to invalid rather than to
	// role.Value. That direction fails closed — the write gets reported —
	// but a package that stopped type-checking is a reason to stop reading the
	// report, not to trust it.
	It("type-check package auth before believing anything below", func() {
		Expect(scanAuth().errs).To(BeEmpty())
	})

	It("route every role write outside the allow-list through role.Federated",
		func() {
			var offenders []string
			for _, w := range scanAuth().roleWrites() {
				_, writer := roleWriters[w.fn]
				_, carrier := roleCarriers[w.fn]
				if w.ok || writer || carrier {
					continue
				}
				offenders = append(offenders, w.String())
			}
			Expect(offenders).To(BeEmpty())
		})

	// Without this the guard above passes just as well when the walker has gone
	// blind — a renamed params type, a moved file, a shape it stopped matching.
	// These are the writes it has to keep seeing.
	//
	// The list is two entries rather than the nine it was, and that shrinkage is
	// the point: db's params take a role.Value now, so every write the six
	// user-creating functions make is vouched by the compiler and no longer
	// reaches this walker at all. What is left is what the type change did not
	// reach — an invite's own role, and the JWT's role string.
	It("still see the role writes no type vouches for", func() {
		want := []string{"CreateInvite", "ListUsers"}

		found := map[string]bool{}
		for _, w := range scanAuth().roleWrites() {
			if w.ok {
				continue
			}
			found[w.fn] = true
		}
		Expect(slices.Sorted(maps.Keys(found))).To(Equal(want))
	})

	// And the same fixed point for the one write vouched for by shape rather
	// than by name. If rowRole stopped matching, issueToken's mirror would
	// simply become an offender and the spec above would fail; if it started
	// matching everything, this is what still holds it to a real site.
	It("still see the JWT mirror it vouches for by shape", func() {
		var vouched []string
		for _, w := range scanAuth().roleWrites() {
			if w.ok {
				vouched = append(vouched, w.fn)
			}
		}
		Expect(vouched).To(ContainElement("issueToken"))
	})

	// The allow-list's whole justification is "no OIDC login reaches it". This
	// is the spec that makes that a fact about the code rather than a sentence
	// about it.
	It("keep every role writer out of an OIDC login's reach", func() {
		reached := reachableFrom(scanAuth().callGraph(), "LoginOIDC")
		var offenders []string
		for fn := range roleWriters {
			if reached[fn] {
				offenders = append(offenders, fn)
			}
		}
		Expect(offenders).To(BeEmpty())
	})

	// And this is what stops that from passing because the walker reached
	// nothing at all — a renamed entry point, a receiver spelled differently, a
	// call shape it stopped following.
	It("still see the calls an OIDC login makes", func() {
		reached := reachableFrom(scanAuth().callGraph(), "LoginOIDC")
		for _, fn := range []string{
			"syncOIDCProfile",
			"syncOIDCRole",
			"issueToken",
			"findOIDCProvider",
			"oidcClaimRoles",
			"CreateSession",
		} {
			Expect(reached).To(HaveKey(fn))
		}
	})

	// Every entry is an escalation a reviewer actually landed on a green build,
	// spliced back into the function it was landed in and replayed against the
	// walker that now has to see it. The one shape missing is round 5's
	// `var esc oidcRole; esc.role = …`, which no longer parses to anything
	// meaningful: role.Value's field is unexported, so that spelling is a
	// compile error in package auth rather than a write this can catch.
	DescribeTable(
		"catch a role write injected into an OIDC path",
		func(stmts, decls string) {
			scan := scanAuth(patch{
				anchor: syncOIDCProfileAnchor,
				stmts:  stmts,
				decls:  decls,
			})
			var caught []string
			for _, w := range scan.roleWrites() {
				if w.fn == "syncOIDCProfile" && !w.ok {
					caught = append(caught, w.String())
				}
			}
			Expect(caught).NotTo(BeEmpty())
		},
		Entry("field assignment, the shape round 4's guard missed",
			"\thand := entuser.RoleAdmin\n\tparams.Role = &hand", ""),
		Entry("composite literal",
			"\t_ = db.CreateUserParams{Role: entuser.RoleAdmin}", ""),
		Entry("a params type declared in this package, the shape round 5 missed",
			"\thand := \"admin\"\n\t_ = UserPatch{Role: &hand}", ""),
		Entry("a params type nobody has invented yet",
			"\t_ = db.PromoteAccountArgs{Role: entuser.RoleAdmin}", ""),
		Entry("the JWT's own claim, forged instead of mirrored",
			"\t_ = Claims{Role: \"admin\"}", ""),
		Entry("ent builder",
			"\ts.db.User.UpdateOne(nil).SetRole(entuser.RoleAdmin)", ""),
		Entry("round 6's bypass: a foreign type spelling the blessed exit",
			"\tparams.Role = shadowRole{r: entuser.RoleAdmin}.EntPtr()",
			"type shadowRole struct{ r entuser.Role }\n\n"+
				"func (f shadowRole) EntRolePtr() *entuser.Role { return &f.r }"),
		Entry(
			"the same trick on the other exit, into a create",
			"\t_ = db.CreateUserParams{Role: shadowRole{r: entuser.RoleAdmin}.Ent()}",
			"type shadowRole struct{ r entuser.Role }\n\n"+
				"func (f shadowRole) EntRole() entuser.Role { return f.r }",
		),
		Entry("a role mirrored off a constant instead of a row",
			"\t_ = Claims{Role: string(entuser.RoleAdmin)}", ""),
		Entry("a role mirrored off some other table's row",
			"\tvar inv *ent.Invite\n\t_ = Claims{Role: string(inv.Role)}", ""),
	)

	// The other half of round 5's bypass: the write itself sat in UpdateUser,
	// which is allow-listed and blameless, and LoginOIDC simply called it.
	DescribeTable(
		"catch a call to a role writer injected into an OIDC path",
		func(stmts, decls string) {
			scan := scanAuth(patch{
				anchor: loginOIDCAnchor,
				stmts:  stmts,
				decls:  decls,
			})
			reached := reachableFrom(scan.callGraph(), "LoginOIDC")
			var found []string
			for fn := range roleWriters {
				if reached[fn] {
					found = append(found, fn)
				}
			}
			Expect(found).NotTo(BeEmpty())
		},
		Entry("called directly", "\ts.UpdateUser(ctx, 1, UserPatch{})", ""),
		Entry("called one hop away, through a helper",
			"\ts.injectedHelper(ctx)",
			"func (s *auth) injectedHelper(ctx context.Context) {\n"+
				"\ts.CreateUserDirect(ctx, \"\", \"\", \"admin\", \"\")\n}"),
		Entry("called through a copy of the receiver",
			"\tesc := s\n\tesc.UpdateUser(ctx, 1, UserPatch{})", ""),
		Entry("stored as a method value, the dodge round 6's graph missed",
			"\tup := s.UpdateUser\n\t_ = up", ""),
		Entry("named as a method expression",
			"\tup := (*auth).UpdateUser\n\t_ = up", ""),
		Entry("deferred", "\tdefer s.UpdateUser(ctx, 1, UserPatch{})", ""),
		Entry("started in a goroutine",
			"\tgo s.UpdateUser(ctx, 1, UserPatch{})", ""),
	)

	// The uncovered list, injected rather than asserted. CLAUDE.md carries the
	// same four shapes in prose, and CLAUDE.md's own line says not to restate
	// that list without injecting the shape and watching it fail. This is where
	// that happens. A failure here means one of them is now caught and the list
	// is stale — a docs fix, not a regression.
	DescribeTable(
		"leave uncovered, on purpose",
		func(p patch, fn, shape string) {
			writes := scanAuth(p).roleWrites()
			// "Not caught" is only worth something once the injection has
			// landed: an anchor that stopped matching would report the same
			// silence as a shape the guard genuinely cannot see.
			Expect(roleWritesIn(writes, fn)).To(BeNumerically(">",
				roleWritesIn(scanAuth().roleWrites(), fn)))

			var caught []string
			for _, w := range writes {
				_, writer := roleWriters[w.fn]
				_, carrier := roleCarriers[w.fn]
				if w.fn != fn || w.ok || writer || carrier {
					continue
				}
				caught = append(caught, w.String())
			}
			Expect(caught).To(BeEmpty(), "%s is now caught", shape)
		},
		// unsafe rewrites the field the package boundary protects, so the value
		// leaves through the real exit on the real type and every check here
		// passes on it.
		Entry("a role forged through unsafe",
			patch{
				anchor: syncOIDCProfileAnchor,
				stmts: "\tvar esc approle.Value\n" +
					"\t*(*string)(unsafe.Pointer(&esc)) = \"admin\"\n" +
					"\tparams.Role = &esc",
				imports: "import \"unsafe\"",
			},
			"syncOIDCProfile", "unsafe"),
		// Cap named with a provider the request is not authenticating against
		// is capped by that provider's ceiling rather than by none.
		Entry("role.Federated named with a foreign provider",
			patch{
				anchor: syncOIDCProfileAnchor,
				stmts: "\tesc := approle.Federated(\"some-other-provider\", \"admin\")\n" +
					"\tparams.Role = &esc",
			},
			"syncOIDCProfile", "a Cap naming another provider"),
		// A roleCarriers entry is exempt by name, and nothing checks that a
		// login cannot reach it.
		Entry("a forged role inside a roleCarriers function",
			patch{
				anchor: "\tctx, span := tracer.Start(ctx, \"users.list\")",
				stmts: "\thand := user.RoleAdmin\n" +
					"\t_ = db.UpdateUserParams{Role: &hand}",
			},
			"ListUsers", "a write inside ListUsers"),
		// rowRole proves the value came off a user row, not off the row this
		// login authenticated. issueToken reads one user; a second one passes
		// the same shape check.
		Entry("the JWT mirrored off a row that is not the caller's",
			patch{
				anchor: "\ttoken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)",
				before: true,
				stmts: "\tif other, oerr := s.db.FindUserByID(ctx, 1); oerr == nil {\n" +
					"\t\tclaims.Role = string(other.Role)\n\t}",
			},
			"issueToken", "a mirror of another row"),
	)
})

var _ = Describe("findOIDCProvider", Label("unit", "auth"), func() {
	cfg := &config.Config{Auth: config.AuthConfig{OIDC: []config.OIDCConfig{
		{Name: "kc", EmailLinking: config.OIDCEmailLinkingAll},
	}}}

	It("returns the named entry", func() {
		pc, ok := findOIDCProvider(cfg, "kc")
		Expect(ok).To(BeTrue())
		Expect(pc.EmailLinking).To(Equal(config.OIDCEmailLinkingAll))
	})

	It("returns a zero config for an unconfigured provider", func() {
		pc, ok := findOIDCProvider(cfg, "ghost")
		Expect(ok).To(BeFalse())
		Expect(pc.EmailLinking).To(BeEmpty())
	})
})

var _ = Describe("emailLinkingAllowed", Label("unit", "auth"), func() {
	DescribeTable("decides by mode and target role",
		func(mode string, role entuser.Role, want bool) {
			Expect(emailLinkingAllowed(mode, role)).To(Equal(want))
		},
		Entry("unset refuses a member", "", entuser.RoleMember, false),
		Entry(
			"disabled refuses a member",
			config.OIDCEmailLinkingDisabled,
			entuser.RoleMember,
			false,
		),
		Entry(
			"non_admin allows a member",
			config.OIDCEmailLinkingNonAdmin,
			entuser.RoleMember,
			true,
		),
		Entry(
			"non_admin allows a request_only user",
			config.OIDCEmailLinkingNonAdmin,
			entuser.RoleRequestOnly,
			true,
		),
		Entry(
			"non_admin refuses an admin",
			config.OIDCEmailLinkingNonAdmin,
			entuser.RoleAdmin,
			false,
		),
		Entry(
			"all allows an admin",
			config.OIDCEmailLinkingAll,
			entuser.RoleAdmin,
			true,
		),
	)
})

var _ = Describe("claimValue", Label("unit", "auth"), func() {
	It("walks a dotted path several levels deep", func() {
		got := claimValue(map[string]any{
			"a": map[string]any{"b": map[string]any{"c": "deep"}},
		}, "a.b.c")
		Expect(got).To(Equal("deep"))
	})

	It("returns nil for a missing leaf", func() {
		Expect(claimValue(map[string]any{
			"a": map[string]any{"b": "x"},
		}, "a.missing")).To(BeNil())
	})

	It("returns nil for a claim that is absent entirely", func() {
		Expect(claimValue(map[string]any{"a": "x"}, "groups")).To(BeNil())
	})
})

var _ = Describe("LoginOIDC unit", Label("unit", "auth"), func() {
	const ctxType = "*context.valueCtx"

	var (
		ctx       context.Context
		storeMock *dbmocks.MockStore_Expecter
		svc       *auth
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		m, err := New(store)
		svc = m.(*auth)
		Expect(err).NotTo(HaveOccurred())

		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret":    "test-secret",
				"session_ttl":       "1h",
				"registration_mode": "open",
				"oidc_default_role": "member",
			},
		})
	})

	// setupNamedProvider re-seeds the singleton with an auth.oidc entry for
	// "google" carrying the given adoption tier and admin ceiling, plus any
	// extra provider keys. The suite's default config declares no provider at
	// all, which is the stricter case on both axes: neither adoption nor admin
	// is opted in for a provider nobody configured.
	setupNamedProvider := func(
		regMode, emailLinking string,
		allowAdmin bool,
		extra map[string]any,
	) {
		GinkgoHelper()
		provider := map[string]any{
			"name":          "google",
			"issuer":        "https://accounts.google.com",
			"client_id":     "cid",
			"client_secret": "sec",
			"email_linking": emailLinking,
			"allow_admin":   allowAdmin,
		}
		maps.Copy(provider, extra)
		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret":    "test-secret",
				"session_ttl":       "1h",
				"registration_mode": regMode,
				"oidc_default_role": "member",
				"oidc":              []any{provider},
			},
		})
	}

	setupProvider := func(emailLinking string) {
		GinkgoHelper()
		setupNamedProvider("open", emailLinking, false, nil)
	}

	// setupInviteProvider keeps registration_mode on invite, so the provider is
	// configured for the branch that consumes one.
	setupInviteProvider := func(allowAdmin bool) {
		GinkgoHelper()
		setupNamedProvider(
			"invite",
			config.OIDCEmailLinkingDisabled,
			allowAdmin,
			nil,
		)
	}

	// setupRoleMappedProvider is setupProvider plus a groups → role mapping,
	// the combination that lets claims decide the role a login lands on.
	setupRoleMappedProvider := func(emailLinking string) {
		GinkgoHelper()
		setupNamedProvider("open", emailLinking, false, map[string]any{
			"role_claim":   "groups",
			"role_mapping": map[string]any{"streamline-admins": "admin"},
		})
	}

	// newUserTx wires the transaction the new-user provisioning path opens
	// around user creation, identity creation and invite consumption.
	newUserTx := func() *dbmocks.MockTx {
		GinkgoHelper()
		tx := dbmocks.NewMockTx(GinkgoT())
		storeMock.Tx(mock.AnythingOfType(ctxType)).Return(tx, nil).Once()
		return tx
	}

	When("an OIDC identity already exists", func() {
		It("logs in the linked user without re-syncing when claims match", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "U",
				Role:        entuser.RoleMember,
			}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u).To(Equal(owner))
			Expect(tok).NotTo(BeEmpty())
		})

		It("logs in an admin whose identity is linked, with no opt-in", func() {
			owner := &ent.User{
				ID:         1,
				Email:      "admin@streamline.local",
				Role:       entuser.RoleAdmin,
				AuthMethod: entuser.AuthMethodBoth,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(&ent.OIDCIdentity{
					ID:    1,
					Edges: ent.OIDCIdentityEdges{Owner: owner},
				}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"admin@streamline.local",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u).To(Equal(owner))
			Expect(tok).NotTo(BeEmpty())
		})

		It("logs the last admin in unchanged when the guard refuses", func() {
			setupNamedProvider("open", config.OIDCEmailLinkingDisabled, false,
				map[string]any{
					"role_claim":   "groups",
					"role_mapping": map[string]any{"streamline-staff": "member"},
				})
			owner := &ent.User{
				ID:         1,
				Email:      "boss@x.com",
				Role:       entuser.RoleAdmin,
				AuthMethod: entuser.AuthMethodBoth,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(&ent.OIDCIdentity{
					ID:    1,
					Edges: ent.OIDCIdentityEdges{Owner: owner},
				}, nil).
				Once()
			storeMock.UpdateUserRole(mock.AnythingOfType(ctxType), uint32(1),
				mock.Anything).
				Return(nil, db.ErrLastAdmin).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"boss@x.com",
				"",
				true,
				map[string]any{"groups": []any{"streamline-staff"}},
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleAdmin))
			Expect(tok).NotTo(BeEmpty())
		})

		It("syncs display_name when the claim differs and reloads the user", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "Old",
				Role:        entuser.RoleMember,
			}
			reloaded := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "New",
				Role:        entuser.RoleMember,
			}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.DisplayName != nil && *p.DisplayName == "New" &&
						p.Email == nil
				})).
				Return(reloaded, nil).Once()
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(reloaded, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"New",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.DisplayName).To(Equal("New"))
		})

		It("skips email update when the claim collides with another user", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "old@x.com",
				DisplayName: "U",
				Role:        entuser.RoleMember,
			}
			collision := &ent.User{ID: 2, Email: "new@x.com"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "new@x.com").
				Return(collision, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"new@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Email).To(Equal("old@x.com"))
		})

		It("clears lockout state when the user was previously locked", func() {
			locked := time.Now().Add(5 * time.Minute)
			lastFail := time.Now().Add(-1 * time.Minute)
			owner := &ent.User{
				ID: 1, Email: "u@x.com", DisplayName: "U",
				FailedLoginCount:  3,
				LastFailedLoginAt: &lastFail,
				LockedUntil:       &locked,
			}
			reloaded := &ent.User{ID: 1, Email: "u@x.com", DisplayName: "U"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.FailedLoginCount != nil && *p.FailedLoginCount == 0 &&
						p.ClearLastFailedLoginAt && p.ClearLockedUntil
				})).
				Return(reloaded, nil).Once()
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(reloaded, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("the identity lookup errors with non-NotFound", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, errors.New("query fail")).
				Once()
			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("query oidc identity")))
		})
	})

	When("email is unverified", func() {
		It("rejects with ErrOIDCEmailUnverified", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				false,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCEmailUnverified))
		})
	})

	When("a user with the same email already exists", func() {
		// The refusal specs deliberately register no CreateOIDCIdentity /
		// CreateSession expectation: mockery fails the spec if LoginOIDC
		// reaches either, so "no identity was linked, no session issued" is
		// asserted by the absence.
		It("refuses to adopt the account when the provider has no opt-in", func() {
			existing := &ent.User{
				ID:         1,
				Email:      "victim@x.com",
				Role:       entuser.RoleMember,
				AuthMethod: entuser.AuthMethodLocal,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "victim@x.com").
				Return(existing, nil).Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				"victim@x.com",
				"Not The Victim",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
			Expect(u).To(BeNil())
			Expect(tok).To(BeEmpty())
		})

		It("refuses when the provider sets email_linking: disabled", func() {
			setupProvider(config.OIDCEmailLinkingDisabled)
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "victim@x.com").
				Return(&ent.User{
					ID:         1,
					Email:      "victim@x.com",
					Role:       entuser.RoleMember,
					AuthMethod: entuser.AuthMethodLocal,
				}, nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				"victim@x.com",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
		})

		It("refuses to adopt an admin under email_linking: non_admin", func() {
			setupProvider(config.OIDCEmailLinkingNonAdmin)
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "admin@streamline.local").
				Return(&ent.User{
					ID:         1,
					Email:      "admin@streamline.local",
					Role:       entuser.RoleAdmin,
					AuthMethod: entuser.AuthMethodLocal,
				}, nil).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				"admin@streamline.local",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
			Expect(u).To(BeNil())
			Expect(tok).To(BeEmpty())
		})

		It("adopts an admin only under email_linking: all", func() {
			setupProvider(config.OIDCEmailLinkingAll)
			existing := &ent.User{
				ID:         1,
				Email:      "admin@streamline.local",
				Role:       entuser.RoleAdmin,
				AuthMethod: entuser.AuthMethodLocal,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "admin@streamline.local").
				Return(existing, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("db.UpdateUserParams")).
				Return(&ent.User{ID: 1, AuthMethod: entuser.AuthMethodBoth}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"admin@streamline.local",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("links identity, promotes local→both, and logs in", func() {
			setupProvider(config.OIDCEmailLinkingNonAdmin)
			existing := &ent.User{
				ID:         1,
				Email:      "u@x.com",
				Role:       entuser.RoleMember,
				AuthMethod: entuser.AuthMethodLocal,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateOIDCIdentityParams) bool {
				return p.Provider == "google" && p.OwnerID == 1
			})).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.AuthMethod != nil && *p.AuthMethod == entuser.AuthMethodBoth
			})).
				Return(&ent.User{ID: 1, AuthMethod: entuser.AuthMethodBoth}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.AuthMethod).To(Equal(entuser.AuthMethodBoth))
		})

		It("does not promote when auth_method is already oidc", func() {
			setupProvider(config.OIDCEmailLinkingNonAdmin)
			existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodOidc}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It(
			"adopts a member under non_admin without applying an admin claim",
			func() {
				setupRoleMappedProvider(config.OIDCEmailLinkingNonAdmin)
				existing := &ent.User{
					ID:         1,
					Email:      "victim@x.com",
					Role:       entuser.RoleMember,
					AuthMethod: entuser.AuthMethodLocal,
				}
				storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
					Return(nil, &ent.NotFoundError{}).
					Once()
				storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "victim@x.com").
					Return(existing, nil).
					Once()
				storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
					Return(&ent.OIDCIdentity{ID: 1}, nil).
					Once()
				// Only the local → both promotion; no role write reaches the
				// store, which is what the mock asserts by not expecting one.
				storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
					mock.MatchedBy(func(p db.UpdateUserParams) bool {
						return p.Role == nil && p.AuthMethod != nil
					})).
					Return(&ent.User{
						ID:         1,
						Email:      "victim@x.com",
						Role:       entuser.RoleMember,
						AuthMethod: entuser.AuthMethodBoth,
					}, nil).
					Once()
				storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
					Return(&ent.Session{ID: 1}, nil).
					Once()

				u, tok, err := svc.LoginOIDC(
					ctx,
					"google",
					"sub-evil",
					"victim@x.com",
					"",
					true,
					map[string]any{"groups": []any{"streamline-admins"}},
					SessionMeta{},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(tok).NotTo(BeEmpty())
				Expect(u.Role).To(Equal(entuser.RoleMember))
			},
		)

		// The second half of the request-split attack: the link now exists, so
		// step 1 runs and syncOIDCRole is reached with the admin claim the
		// first request withheld.
		It(
			"refuses to re-sync an adopted account onto admin under non_admin",
			func() {
				setupRoleMappedProvider(config.OIDCEmailLinkingNonAdmin)
				owner := &ent.User{
					ID:         1,
					Email:      "victim@x.com",
					Role:       entuser.RoleMember,
					AuthMethod: entuser.AuthMethodBoth,
				}
				storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
					Return(&ent.OIDCIdentity{
						ID:    1,
						Edges: ent.OIDCIdentityEdges{Owner: owner},
					}, nil).
					Once()
				storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
					Return(&ent.Session{ID: 1}, nil).
					Once()

				u, tok, err := svc.LoginOIDC(
					ctx,
					"google",
					"sub-evil",
					"victim@x.com",
					"",
					true,
					map[string]any{"groups": []any{"streamline-admins"}},
					SessionMeta{},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(tok).NotTo(BeEmpty())
				Expect(u.Role).To(Equal(entuser.RoleMember))
			},
		)

		It("does not apply claim-mapped roles on the adoption request", func() {
			setupRoleMappedProvider(config.OIDCEmailLinkingAll)
			existing := &ent.User{
				ID:         1,
				Email:      "victim@x.com",
				Role:       entuser.RoleMember,
				AuthMethod: entuser.AuthMethodLocal,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "victim@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			// Only the auth_method promotion may write: a Role in these params
			// is the escalation this spec guards against.
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.Role == nil && p.AuthMethod != nil
			})).
				Return(&ent.User{
					ID:         1,
					Email:      "victim@x.com",
					Role:       entuser.RoleMember,
					AuthMethod: entuser.AuthMethodBoth,
				}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				"victim@x.com",
				"",
				true,
				map[string]any{"groups": []any{"streamline-admins"}},
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleMember))
		})

		It("looks the account up on the trimmed, lowercased claim email", func() {
			setupProvider(config.OIDCEmailLinkingDisabled)
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-evil").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "victim@x.com").
				Return(&ent.User{ID: 1, Email: "victim@x.com"}, nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-evil",
				" Victim@X.com ",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCLinkNotAllowed))
		})

		It("wraps CreateOIDCIdentity errors", func() {
			setupProvider(config.OIDCEmailLinkingNonAdmin)
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(&ent.User{ID: 1, AuthMethod: entuser.AuthMethodLocal}, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(nil, errors.New("link fail")).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("link oidc identity")))
		})
	})

	When("no user matches and registration_mode is open", func() {
		It("creates a new user with the default role", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
					return p.Email == "u@x.com" &&
						p.Role.String() == string(entuser.RoleMember) &&
						p.AuthMethod == entuser.AuthMethodOidc
				})).
				Return(&ent.User{ID: 1, Email: "u@x.com"}, nil).
				Once()
			tx.EXPECT().
				CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			tx.EXPECT().Commit().Return(nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("registration_mode is disabled and table is non-empty", func() {
		It("rejects with ErrOIDCRegDisabled", func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret":    "test-secret",
					"session_ttl":       "1h",
					"registration_mode": "disabled",
					"oidc_default_role": "member",
				},
			})
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCRegDisabled))
		})
	})

	When("registration_mode is invite", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret":    "test-secret",
					"session_ttl":       "1h",
					"registration_mode": "invite",
					"oidc_default_role": "member",
				},
			})
		})

		It("rejects with ErrOIDCNoInvite when no matching invite exists", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(nil, &ent.NotFoundError{}).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCNoInvite))
		})

		// inviteTx wires the whole invite-consuming provisioning transaction and
		// asserts the role the user is created with.
		inviteTx := func(inviteRole invite.Role, want entuser.Role) {
			GinkgoHelper()
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9, Role: inviteRole, ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
					return p.Role.String() == string(want)
				})).
				Return(&ent.User{ID: 1, Role: want}, nil).
				Once()
			tx.EXPECT().
				CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			tx.EXPECT().
				ConsumeInvite(mock.AnythingOfType(ctxType), uint32(9), uint32(1), mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			tx.EXPECT().Commit().Return(nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()
		}

		It("consumes the invite and uses its role", func() {
			inviteTx(invite.RoleMember, entuser.RoleMember)

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleMember))
		})

		// The invite role reaches this path over a channel the provider holds
		// the far end of, so the ceiling applies to it like every other source.
		// The alternative was an exception nowhere in the docs, which is how
		// the last three rounds ended up with a table promising "never" next to
		// a path that yielded admin.
		It("caps an admin invite for a provider without allow_admin", func() {
			setupInviteProvider(false)
			inviteTx(invite.RoleAdmin, entuser.RoleMember)

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleMember))
		})

		It("honours an admin invite once the provider carries allow_admin", func() {
			setupInviteProvider(true)
			inviteTx(invite.RoleAdmin, entuser.RoleAdmin)

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleAdmin))
		})

		It("leaves the invite unconsumed when user creation fails", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9, Role: invite.RoleMember, ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).
				Once()
			tx.EXPECT().Rollback().Return(nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})

		It("wraps ConsumeInvite errors and rolls back", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9, Role: invite.RoleMember, ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()
			tx.EXPECT().
				CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			tx.EXPECT().
				ConsumeInvite(mock.AnythingOfType(ctxType), uint32(9), uint32(1), mock.AnythingOfType("time.Time")).
				Return(errors.New("consume fail")).
				Once()
			tx.EXPECT().Rollback().Return(nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("consume invite")))
		})

		It(
			"rejects with ErrOIDCNoInvite when the invite was consumed concurrently",
			func() {
				storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
					Return(nil, &ent.NotFoundError{}).
					Once()
				storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
					Return(nil, &ent.NotFoundError{}).Once()
				storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
					Return(&ent.Invite{ID: 9, Role: invite.RoleMember, ExpiresAt: time.Now().Add(time.Hour)}, nil).
					Once()
				tx := newUserTx()
				tx.EXPECT().
					CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
					Return(&ent.User{ID: 1}, nil).
					Once()
				tx.EXPECT().
					CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
					Return(&ent.OIDCIdentity{ID: 1}, nil).
					Once()
				tx.EXPECT().
					ConsumeInvite(mock.AnythingOfType(ctxType), uint32(9), uint32(1), mock.AnythingOfType("time.Time")).
					Return(db.ErrInviteUsed).
					Once()
				tx.EXPECT().Rollback().Return(nil).Once()

				_, _, err := svc.LoginOIDC(
					ctx,
					"google",
					"sub-1",
					"u@x.com",
					"U",
					true,
					nil,
					SessionMeta{},
				)
				Expect(err).To(MatchError(ErrOIDCNoInvite))
			},
		)
	})

	When("CreateUser fails for the new-user path", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).
				Once()
			tx.EXPECT().Rollback().Return(nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})
	})

	When("issueToken fails for the existing-identity path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			owner := &ent.User{ID: 7, Email: "u@x.com", DisplayName: "U"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(owner))
			Expect(tok).To(BeEmpty())
		})
	})

	When("FindUserByEmail returns a non-NotFound error", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, errors.New("query fail")).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("query user by email")))
		})
	})

	When(
		"UpdateUser fails for the linked-existing path (auth_method promotion)",
		func() {
			It("wraps the error", func() {
				setupProvider(config.OIDCEmailLinkingNonAdmin)
				existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodLocal}
				storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
					Return(nil, &ent.NotFoundError{}).
					Once()
				storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
					Return(existing, nil).Once()
				storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
					Return(&ent.OIDCIdentity{ID: 1}, nil).
					Once()
				storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("db.UpdateUserParams")).
					Return(nil, errors.New("update fail")).
					Once()

				_, _, err := svc.LoginOIDC(
					ctx,
					"google",
					"sub-1",
					"u@x.com",
					"U",
					true,
					nil,
					SessionMeta{},
				)
				Expect(err).To(MatchError(ContainSubstring("update auth_method")))
			})
		},
	)

	When("issueToken fails for the linked-existing path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			setupProvider(config.OIDCEmailLinkingNonAdmin)
			existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodOidc}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(existing))
			Expect(tok).To(BeEmpty())
		})
	})

	When("CreateOIDCIdentity fails after CreateUser succeeded", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()
			tx.EXPECT().
				CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(nil, errors.New("create id fail")).
				Once()
			tx.EXPECT().Rollback().Return(nil).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create identity")))
		})
	})

	When("issueToken fails for the new-user path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			created := &ent.User{ID: 1, Email: "u@x.com"}
			tx := newUserTx()
			tx.EXPECT().
				CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(created, nil).
				Once()
			tx.EXPECT().
				CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			tx.EXPECT().Commit().Return(nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(created))
			Expect(tok).To(BeEmpty())
		})
	})
})
