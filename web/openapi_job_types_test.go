package web

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

// unknownTypeMarker is the substring of the error jobFromRequest's default
// arm returns. It is the only signal that separates "the switch refused this
// token" from "the switch dispatched and construction failed later", so a
// reworded error must be reflected here or this test silently accepts
// everything.
const unknownTypeMarker = "unknown job type"

// candidateJobTypes is the population offered to the handler. It deliberately
// includes jobTypeService, which is documented as not creatable and must come
// back rejected, and a token no code path knows.
//
// This slice is the one hand-kept part of the test: a new jobType* constant
// has to be added here, or it goes unexercised.
func candidateJobTypes() []string {
	return []string{
		jobTypeRun,
		jobTypeExec,
		jobTypeLocal,
		jobTypeService,
		jobTypeCompose,
		"service-run",
		"nonesuch",
	}
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
			} `yaml:"JobRequest"`
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
func acceptedJobTypes(t *testing.T) []string {
	t.Helper()

	s := &Server{}
	var accepted []string
	for _, typ := range candidateJobTypes() {
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

	accepted := acceptedJobTypes(t)

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

	if _, err := (&Server{}).jobFromRequest(&jobRequest{Name: "guard"}); err != nil &&
		strings.Contains(err.Error(), unknownTypeMarker) {
		t.Errorf("an empty type is rejected by the switch; the spec's "+
			"%q description no longer holds", "omitted means local")
	}
}
