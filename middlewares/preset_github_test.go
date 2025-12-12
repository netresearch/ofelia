package middlewares

import (
	"testing"

	. "gopkg.in/check.v1"
)

type SuitePresetGitHub struct {
	BaseSuite
}

var _ = Suite(&SuitePresetGitHub{})

func (s *SuitePresetGitHub) TestIsGitHubShorthand_True(c *C) {
	c.Assert(IsGitHubShorthand("gh:org/repo/path.yaml"), Equals, true)
	c.Assert(IsGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml"), Equals, true)
}

func (s *SuitePresetGitHub) TestIsGitHubShorthand_False(c *C) {
	c.Assert(IsGitHubShorthand("slack"), Equals, false)
	c.Assert(IsGitHubShorthand("https://example.com"), Equals, false)
	c.Assert(IsGitHubShorthand("/path/to/file.yaml"), Equals, false)
	c.Assert(IsGitHubShorthand(""), Equals, false)
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_SimpleFormat(c *C) {
	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/netresearch/ofelia-presets/main/slack.yaml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_WithVersion(c *C) {
	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml@v1.0.0")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/netresearch/ofelia-presets/v1.0.0/slack.yaml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_WithBranch(c *C) {
	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml@develop")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/netresearch/ofelia-presets/develop/slack.yaml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_NestedPath(c *C) {
	url, err := ParseGitHubShorthand("gh:org/repo/notifications/slack.yaml")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/org/repo/main/notifications/slack.yaml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_AutoAddYAML(c *C) {
	url, err := ParseGitHubShorthand("gh:org/repo/slack")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/org/repo/main/slack.yaml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_YMLExtension(c *C) {
	url, err := ParseGitHubShorthand("gh:org/repo/slack.yml")

	c.Assert(err, IsNil)
	c.Assert(url, Equals, "https://raw.githubusercontent.com/org/repo/main/slack.yml")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_InvalidFormat(c *C) {
	_, err := ParseGitHubShorthand("gh:")

	c.Assert(err, NotNil)
}

func (s *SuitePresetGitHub) TestParseGitHubShorthand_NotGitHub(c *C) {
	_, err := ParseGitHubShorthand("https://example.com")

	c.Assert(err, NotNil)
}

func (s *SuitePresetGitHub) TestParseGitHubShorthandDetails(c *C) {
	gh, err := ParseGitHubShorthandDetails("gh:netresearch/ofelia-presets/notifications/slack.yaml@v1.0.0")

	c.Assert(err, IsNil)
	c.Assert(gh, NotNil)
	c.Assert(gh.Org, Equals, "netresearch")
	c.Assert(gh.Repo, Equals, "ofelia-presets")
	c.Assert(gh.Path, Equals, "notifications/slack.yaml")
	c.Assert(gh.Version, Equals, "v1.0.0")
}

func (s *SuitePresetGitHub) TestParseGitHubShorthandDetails_DefaultVersion(c *C) {
	gh, err := ParseGitHubShorthandDetails("gh:org/repo/path.yaml")

	c.Assert(err, IsNil)
	c.Assert(gh.Version, Equals, "main")
}

func (s *SuitePresetGitHub) TestIsVersioned_True(c *C) {
	c.Assert(IsVersioned("gh:org/repo/path@v1.0.0"), Equals, true)
	c.Assert(IsVersioned("gh:org/repo/path@main"), Equals, true)
}

func (s *SuitePresetGitHub) TestIsVersioned_False(c *C) {
	c.Assert(IsVersioned("gh:org/repo/path"), Equals, false)
	c.Assert(IsVersioned("slack"), Equals, false)
}

func (s *SuitePresetGitHub) TestFormatGitHubShorthand(c *C) {
	shorthand := FormatGitHubShorthand("netresearch", "ofelia-presets", "slack.yaml", "v1.0.0")
	c.Assert(shorthand, Equals, "gh:netresearch/ofelia-presets/slack.yaml@v1.0.0")
}

func (s *SuitePresetGitHub) TestFormatGitHubShorthand_DefaultVersion(c *C) {
	shorthand := FormatGitHubShorthand("netresearch", "ofelia-presets", "slack.yaml", "main")
	c.Assert(shorthand, Equals, "gh:netresearch/ofelia-presets/slack.yaml")
}

func (s *SuitePresetGitHub) TestFormatGitHubShorthand_NoPath(c *C) {
	shorthand := FormatGitHubShorthand("org", "repo", "", "v1.0.0")
	c.Assert(shorthand, Equals, "gh:org/repo@v1.0.0")
}

func (s *SuitePresetGitHub) TestExtractVersionFromShorthand(c *C) {
	c.Assert(ExtractVersionFromShorthand("gh:org/repo/path@v1.0.0"), Equals, "v1.0.0")
	c.Assert(ExtractVersionFromShorthand("gh:org/repo/path@main"), Equals, "main")
	c.Assert(ExtractVersionFromShorthand("gh:org/repo/path"), Equals, "")
}

func (s *SuitePresetGitHub) TestStripVersionFromShorthand(c *C) {
	c.Assert(StripVersionFromShorthand("gh:org/repo/path@v1.0.0"), Equals, "gh:org/repo/path")
	c.Assert(StripVersionFromShorthand("gh:org/repo/path"), Equals, "gh:org/repo/path")
}

func (s *SuitePresetGitHub) TestIsSemanticVersion(c *C) {
	c.Assert(IsSemanticVersion("v1.0.0"), Equals, true)
	c.Assert(IsSemanticVersion("1.0.0"), Equals, true)
	c.Assert(IsSemanticVersion("v2.3.4"), Equals, true)
	c.Assert(IsSemanticVersion("main"), Equals, false)
	c.Assert(IsSemanticVersion("develop"), Equals, false)
	c.Assert(IsSemanticVersion("feature/test"), Equals, false)
}

func (s *SuitePresetGitHub) TestIsBranch(c *C) {
	c.Assert(IsBranch("main"), Equals, true)
	c.Assert(IsBranch("master"), Equals, true)
	c.Assert(IsBranch("develop"), Equals, true)
	c.Assert(IsBranch("feature/test"), Equals, true)
	c.Assert(IsBranch("fix/bug"), Equals, true)
	c.Assert(IsBranch("release/1.0"), Equals, true)
	c.Assert(IsBranch("v1.0.0"), Equals, false)
}

func (s *SuitePresetGitHub) TestValidateGitHubShorthand(c *C) {
	c.Assert(ValidateGitHubShorthand("gh:org/repo/path.yaml"), IsNil)
	c.Assert(ValidateGitHubShorthand("gh:org/repo/path.yaml@v1.0.0"), IsNil)
	c.Assert(ValidateGitHubShorthand("slack"), NotNil)
	c.Assert(ValidateGitHubShorthand("https://example.com"), NotNil)
}

// Standard Go testing
func TestGitHubShorthand_RoundTrip(t *testing.T) {
	// Test that parsing and formatting are consistent
	original := "gh:netresearch/ofelia-presets/slack.yaml@v1.0.0"
	details, err := ParseGitHubShorthandDetails(original)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	reconstructed := FormatGitHubShorthand(details.Org, details.Repo, details.Path, details.Version)
	if reconstructed != original {
		t.Errorf("Round trip failed: got %s, want %s", reconstructed, original)
	}
}

func TestGitHubShorthand_URLGeneration(t *testing.T) {
	testCases := []struct {
		shorthand   string
		expectedURL string
	}{
		{
			"gh:org/repo/file.yaml",
			"https://raw.githubusercontent.com/org/repo/main/file.yaml",
		},
		{
			"gh:org/repo/file.yaml@v1.0.0",
			"https://raw.githubusercontent.com/org/repo/v1.0.0/file.yaml",
		},
		{
			"gh:org/repo/dir/file.yaml",
			"https://raw.githubusercontent.com/org/repo/main/dir/file.yaml",
		},
	}

	for _, tc := range testCases {
		url, err := ParseGitHubShorthand(tc.shorthand)
		if err != nil {
			t.Errorf("Failed to parse %s: %v", tc.shorthand, err)
			continue
		}

		if url != tc.expectedURL {
			t.Errorf("For %s: got %s, want %s", tc.shorthand, url, tc.expectedURL)
		}
	}
}
