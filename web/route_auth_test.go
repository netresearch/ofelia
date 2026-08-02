// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package web

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// The authorization boundary is one prefix rule plus an allowlist in
// authMiddleware, so a route registered outside /api/ ships reachable without a
// token and nothing else notices. These tests close that gap:
//
//   - TestRouteAuthExpectations replays every entry of the route table — the
//     same slice both registration sites consume — against the real middleware
//     chain and holds it to its declared `public` flag.
//   - TestPublicRoutesAreExactlyDeclared pins the set of `public: true` routes
//     to a literal list, so growing the token-free surface requires editing
//     this file deliberately.
//   - TestRouteRegistrationIsCentralized fails when a route is registered
//     anywhere — including inside newMux — other than through the route table,
//     which would put it out of reach of TestRouteAuthExpectations.
//   - TestAuthMiddlewarePathDecisionsMatchAllowlist fails when authMiddleware
//     itself dispatches on a path beyond its declared exemptions, closing the
//     "serve it straight from the middleware, no mux registration at all" hole.

func newAuthTestServer(t *testing.T) (*Server, *HealthChecker) {
	t.Helper()

	authCfg := &SecureAuthConfig{
		Enabled:      true,
		Username:     "admin",
		PasswordHash: "$2a$04$placeholder",
		SecretKey:    "test-secret-key-32-bytes-long!!!",
		TokenExpiry:  24,
		MaxAttempts:  5,
	}
	srv := NewServerWithAuth("", core.NewScheduler(newDiscardLogger()), nil, nil, authCfg)
	require.NotNil(t, srv, "NewServerWithAuth returned nil")

	hc := NewHealthChecker(nil, nil, "test")
	srv.RegisterHealthEndpoints(hc)

	t.Cleanup(func() {
		srv.rl.close()
		srv.tokenManager.Close()
	})
	return srv, hc
}

// TestRouteAuthExpectations asserts, for every route the server registers, that
// a request without a token is rejected with 401 unless the route declares
// itself public — and that a valid token gets past the middleware everywhere.
func TestRouteAuthExpectations(t *testing.T) {
	srv, hc := newAuthTestServer(t)
	handler := srv.HTTPServer().Handler

	ui, err := uiHandler()
	require.NoError(t, err)

	routes := srv.routes(hc, ui)
	require.NotEmpty(t, routes)

	token, err := srv.tokenManager.GenerateToken("admin")
	require.NoError(t, err)

	// One client IP per route keeps the 100-request-per-minute rate limiter
	// out of the picture as the table grows.
	probe := func(pattern, authHeader string, ipIndex int) int {
		req := httptest.NewRequest(http.MethodGet, pattern, nil)
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", ipIndex%254+1)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	for i, rt := range routes {
		t.Run(rt.pattern, func(t *testing.T) {
			code := probe(rt.pattern, "", i)
			if rt.public {
				require.NotEqual(t, http.StatusUnauthorized, code,
					"%s is declared public but the middleware demands a token", rt.pattern)
			} else {
				require.Equal(t, http.StatusUnauthorized, code,
					"%s is reachable without a token; move it under /api/ so authMiddleware covers it. "+
						"Only if it genuinely must be anonymous, declare it public AND add it to the "+
						"expected list in TestPublicRoutesAreExactlyDeclared", rt.pattern)
			}

			require.NotEqual(t, http.StatusUnauthorized, probe(rt.pattern, "Bearer "+token, i),
				"%s rejects a valid token", rt.pattern)

			if !rt.public {
				require.Equal(t, http.StatusUnauthorized, probe(rt.pattern, "Bearer not-a-token", i),
					"%s accepts a forged token", rt.pattern)
			}
		})
	}
}

// TestPublicRoutesAreExactlyDeclared pins the set of public (token-free)
// routes to a literal expected list. `public: true` is an escape hatch from
// the auth gate, so growing the set must be a deliberate, reviewed edit of
// this test — never a side effect of a route-table change.
func TestPublicRoutesAreExactlyDeclared(t *testing.T) {
	srv, hc := newAuthTestServer(t)
	ui, err := uiHandler()
	require.NoError(t, err)

	var public []string
	for _, rt := range srv.routes(hc, ui) {
		if rt.public {
			public = append(public, rt.pattern)
		}
	}

	// Deliberately literal strings, not the server's path constants: renaming
	// or adding a public route must surface as a diff in this file.
	want := []string{
		"/api/login",       // token issuance — must be reachable before auth
		"/api/auth/status", // UI probes login state before it has a token
		"/api/csrf-token",  // login form needs the CSRF token before auth
		"/health",          // orchestrator probes
		"/healthz",
		"/ready",
		"/live",
		"/", // static single-page UI shell; all data sits behind /api/
	}
	require.ElementsMatch(t, want, public,
		"the set of public routes changed; if that is intended, update this list deliberately "+
			"and justify why the route must be reachable without a token")
}

// TestRouteRegistrationIsCentralized fails when a route is registered anywhere
// but the table-driven loop in newMux. Outside newMux no Handle/HandleFunc/
// NewServeMux call is allowed at all; inside newMux the only permitted
// registration is `mux.Handle(rt.pattern, rt.handler)` where rt ranges over
// the routes() table — so a route added directly inside newMux (bypassing the
// table, and with it TestRouteAuthExpectations) fails too.
func TestRouteRegistrationIsCentralized(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, parseErr)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "newMux" {
				checkNewMuxRegistrations(t, fset, fn)
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				sel, isSel := call.Fun.(*ast.SelectorExpr)
				if !isSel {
					return true
				}
				switch sel.Sel.Name {
				case "Handle", "HandleFunc", "NewServeMux":
					t.Errorf("%s: %s calls %s outside newMux; register the route in Server.routes instead",
						fset.Position(call.Pos()), fn.Name.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// checkNewMuxRegistrations holds newMux to exactly the table-driven shape: the
// only permitted mux registration is `mux.Handle(rt.pattern, rt.handler)`
// where rt is the value variable of a range over the routes() table. A literal
// pattern — or any other argument shape — is a route that bypasses the table
// and therefore the auth expectations, so it fails here.
func checkNewMuxRegistrations(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()

	// Collect the value variables of `for _, rt := range <recv>.routes(...)`.
	tableVars := map[string]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		call, ok := rng.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "routes" {
			return true
		}
		if id, ok := rng.Value.(*ast.Ident); ok {
			tableVars[id.Name] = true
		}
		return true
	})

	fieldOfTableVar := func(e ast.Expr, field string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != field {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && tableVars[id.Name]
	}

	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Handle", "HandleFunc":
			if len(call.Args) != 2 ||
				!fieldOfTableVar(call.Args[0], "pattern") ||
				!fieldOfTableVar(call.Args[1], "handler") {
				t.Errorf("%s: newMux registers a route outside the route table; "+
					"add it to Server.routes so TestRouteAuthExpectations can hold it to an auth expectation",
					fset.Position(call.Pos()))
			}
		}
		return true
	})
}

// TestAuthMiddlewarePathDecisionsMatchAllowlist closes the hole where a path
// is served straight from authMiddleware with no mux registration at all
// (e.g. `if path == "/internal/dump" { s.configHandler(w, r); return }`),
// which neither the route-table replay nor the registration scan can see.
// It statically asserts three things about authMiddleware:
//
//  1. every equality comparison against the request path targets exactly the
//     declared exemption list — no more, no fewer;
//  2. the only prefix rule is /api/;
//  3. the middleware never reaches into the Server beyond s.tokenManager, so
//     it cannot invoke a handler directly.
//
// A middleware that dispatches by some other mechanism entirely (header
// sniffing, a computed table, …) is beyond a static scan; that residual risk
// is accepted because such code cannot be written by accident, and (3) still
// blocks it from reaching any Server handler.
func TestAuthMiddlewarePathDecisionsMatchAllowlist(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", nil, 0)
	require.NoError(t, err)
	consts := constStrings(f)

	var fn *ast.FuncDecl
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "authMiddleware" {
			fn = fd
			break
		}
	}
	require.NotNil(t, fn, "authMiddleware not found in server.go")

	recv := ""
	if fn.Recv != nil && len(fn.Recv.List) == 1 && len(fn.Recv.List[0].Names) == 1 {
		recv = fn.Recv.List[0].Names[0].Name
	}
	require.NotEmpty(t, recv, "authMiddleware has no named receiver")

	// isPathExpr recognizes the request path: the local `path` variable or a
	// selector ending in .Path (r.URL.Path). If the variable is renamed the
	// collected set comes up empty and the exact-match below fails closed.
	isPathExpr := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			return id.Name == "path"
		}
		if sel, ok := e.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "Path"
		}
		return false
	}

	pathEq := map[string]bool{}
	prefixes := map[string]bool{}

	ast.Inspect(fn, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			if e.Op != token.EQL && e.Op != token.NEQ {
				return true
			}
			var other ast.Expr
			switch {
			case isPathExpr(e.X):
				other = e.Y
			case isPathExpr(e.Y):
				other = e.X
			default:
				return true
			}
			v, ok := resolveString(other, consts)
			if !ok {
				t.Errorf("%s: authMiddleware compares the path against a value that is not a "+
					"string literal or file-level constant; path dispatch must stay auditable",
					fset.Position(e.Pos()))
				return true
			}
			pathEq[v] = true
		case *ast.CallExpr:
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "strings" {
				if sel.Sel.Name != "HasPrefix" || len(e.Args) != 2 || !isPathExpr(e.Args[0]) {
					t.Errorf("%s: authMiddleware uses strings.%s; the only sanctioned path "+
						"predicate is strings.HasPrefix(path, ...)",
						fset.Position(e.Pos()), sel.Sel.Name)
					return true
				}
				v, ok := resolveString(e.Args[1], consts)
				if !ok {
					t.Errorf("%s: authMiddleware prefix-matches the path against a value that is "+
						"not a string literal or file-level constant", fset.Position(e.Pos()))
					return true
				}
				prefixes[v] = true
			}
		case *ast.SelectorExpr:
			// The middleware may use s.tokenManager and nothing else on the
			// Server — in particular no handler methods, which is how a route
			// would be served from here without any registration.
			if id, ok := e.X.(*ast.Ident); ok && id.Name == recv && e.Sel.Name != "tokenManager" {
				t.Errorf("%s: authMiddleware accesses %s.%s; only %s.tokenManager is allowed here — "+
					"anything else can smuggle a handler past the route table",
					fset.Position(e.Pos()), recv, e.Sel.Name, recv)
			}
		}
		return true
	})

	// The exemptions authMiddleware is allowed to make, verbatim. Adding one
	// requires editing this list — and TestPublicRoutesAreExactlyDeclared —
	// deliberately.
	wantEq := []string{
		"/api/login",
		"/api/csrf-token",
		"/api/auth/status",
		"/health",
		"/healthz",
		"/ready",
		"/live",
	}
	require.ElementsMatch(t, wantEq, mapKeys(pathEq),
		"authMiddleware's path allowlist diverged from the declared exemptions")
	require.ElementsMatch(t, []string{"/api/"}, mapKeys(prefixes),
		"authMiddleware's guarded prefix changed; /api/ is the only sanctioned auth boundary")
}

// constStrings collects the file-level string constants of f so the
// middleware scan can resolve `path == pathAPILogin` to its literal value.
func constStrings(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(lit.Value); err == nil {
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

// resolveString resolves expr to a string when it is a string literal or a
// file-level string constant.
func resolveString(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		return v, err == nil
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	}
	return "", false
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
