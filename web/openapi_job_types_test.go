package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/netresearch/ofelia/core"
)

// The published OpenAPI description of a job-create request and the switch
// that implements it drifted apart without anything objecting: the schema
// advertised a type "service-run" that jobFromRequest rejects and jobType
// never emits, and it listed "type" as required although an omitted type is
// a valid request. Both were found by a reviewer reading the diff, which is
// not a mechanism (issue #816).
//
// No workflow validates docs/openapi.yaml, so this test is the whole gate.
// It compares the documented enum against the tokens the real handler takes,
// classifying by the one error the type switch itself produces.

const openAPIRelPath = "../docs/openapi.yaml"

// handlerFile is read from the package directory the test runs in.
const handlerFile = "server.go"

// unknownTypeMarker is the substring of the error jobFromRequest's default
// arm returns. It is the only signal that separates "the switch refused this
// token" from "the switch dispatched and construction failed later".
//
// Rewording that error does not quietly weaken the test: every candidate
// then counts as accepted, and the assertion fails naming "nonesuch" and
// "service-run" among them — a result wrong on sight, which is what the
// sentinel in candidateJobTypes is for. Reflect the new wording here.
const unknownTypeMarker = "unknown job type"

// handlerJobTypes reads the tokens jobFromRequest dispatches on straight out
// of its type switch, resolving the jobType* constants from the same file.
//
// A hand-kept list would have left the guard's main case open: a new arm in
// the handler that nobody documents is exactly the drift this test exists to
// catch, and a list that has to be remembered would not have contained it.
//
// The empty string is dropped. `case "", jobTypeLocal` makes an omitted type
// valid, but "omitted" is not an enum value -- TestOpenAPI_JobTypeNotRequired
// covers that half.
func handlerJobTypes(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, handlerFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", handlerFile, err)
	}

	consts := stringConsts(file)

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "jobFromRequest" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List { // nil for `default`
				if tok, ok := caseToken(expr, consts); ok && tok != "" {
					found = append(found, tok)
				}
			}
			return true
		})
		return false
	})

	if len(found) == 0 {
		t.Fatalf("no case tokens found in jobFromRequest in %s — the handler "+
			"was restructured and this test no longer reads it", handlerFile)
	}
	sort.Strings(found)
	return found
}

// stringConsts maps every top-level string constant in the file to its value,
// so a case written as jobTypeRun resolves to "run".
func stringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := literalString(vs.Values[i]); ok {
					out[name.Name] = lit
				}
			}
		}
	}
	return out
}

func caseToken(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		v, ok := consts[e.Name]
		return v, ok
	default:
		return literalString(expr)
	}
}

func literalString(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// candidateJobTypes is the population offered to the handler: everything the
// handler dispatches on, everything the document claims, and three tokens
// that must come back rejected -- jobTypeService and "service-run", the two
// spellings of the type that is documented as not creatable over the API
// (#816), and "nonesuch", which no code path knows.
func candidateJobTypes(t *testing.T, documented []string) []string {
	t.Helper()

	seen := map[string]bool{}
	var out []string
	for _, tok := range append(append(handlerJobTypes(t), documented...),
		jobTypeService, "service-run", "nonesuch") {
		if tok != "" && !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

// openAPIJobRequest is the slice of the spec this test reads.
type openAPIJobRequest struct {
	Components struct {
		Schemas struct {
			JobRequest struct {
				Required   []string `yaml:"required"`
				Properties struct {
					Type struct {
						Enum []string `yaml:"enum"`
					} `yaml:"type"`
				} `yaml:"properties"`
			} `yaml:"JobRequest"` //nolint:tagliatelle // schema key is spelled by the OpenAPI document, must match exactly
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func readOpenAPIJobRequest(t *testing.T) openAPIJobRequest {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(openAPIRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", openAPIRelPath, err)
	}
	var spec openAPIJobRequest
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse %s: %v", openAPIRelPath, err)
	}
	// A renamed or restructured schema yields a zero value that would make
	// every assertion below vacuous, so it fails here instead.
	if len(spec.Components.Schemas.JobRequest.Properties.Type.Enum) == 0 {
		t.Fatalf("no components.schemas.JobRequest.properties.type.enum in %s — "+
			"the schema moved and this test no longer reads it", openAPIRelPath)
	}
	return spec
}

// acceptedJobTypes returns the candidates the type switch dispatches on,
// driving the real handler rather than a second copy of the list.
//
// A nil provider makes the run and exec constructors fail, which is what we
// want: it proves the switch reached them without needing Docker.
func acceptedJobTypes(t *testing.T, documented []string) []string {
	t.Helper()

	s := &Server{}
	var accepted []string
	for _, typ := range candidateJobTypes(t, documented) {
		_, err := s.jobFromRequest(&jobRequest{Name: "guard", Type: typ})
		if err != nil && strings.Contains(err.Error(), unknownTypeMarker) {
			continue
		}
		accepted = append(accepted, typ)
	}
	sort.Strings(accepted)
	return accepted
}

func TestOpenAPI_JobTypeEnumMatchesHandler(t *testing.T) {
	spec := readOpenAPIJobRequest(t)
	documented := append([]string(nil), spec.Components.Schemas.JobRequest.Properties.Type.Enum...)
	sort.Strings(documented)

	accepted := acceptedJobTypes(t, documented)

	if strings.Join(documented, ",") != strings.Join(accepted, ",") {
		t.Errorf("docs/openapi.yaml documents job types %v, jobFromRequest accepts %v.\n"+
			"Whichever is wrong, the two have to say the same thing — see issue #816.",
			documented, accepted)
	}
}

// TestOpenAPI_JobTypeNotRequired pins the second half of the same drift: an
// omitted type is a valid request that yields a local job, so a generated
// client must not be told the field is mandatory.
func TestOpenAPI_JobTypeNotRequired(t *testing.T) {
	spec := readOpenAPIJobRequest(t)

	for _, field := range spec.Components.Schemas.JobRequest.Required {
		if field == "type" {
			t.Errorf("docs/openapi.yaml marks type as required, but jobFromRequest "+
				"has a `case \"\", %s` arm — an omitted type is a valid body", jobTypeLocal)
		}
	}

	// Not just "the switch did not refuse it" — the request has to succeed
	// and yield a local job, which is what "omitted means local" claims. The
	// weaker form accepted any failure that was not `unknown job type`, so a
	// local constructor that started erroring would have passed it.
	job, err := (&Server{}).jobFromRequest(&jobRequest{Name: "guard"})
	if err != nil {
		t.Fatalf("an empty type is not accepted: %v — the spec's "+
			"%q description no longer holds", err, "omitted means local")
	}
	if _, ok := job.(*core.LocalJob); !ok {
		t.Errorf("an empty type produced %T, want *core.LocalJob", job)
	}
}
