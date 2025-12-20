package middlewares

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGitHubShorthand_True(t *testing.T) {
	t.Parallel()

	assert.True(t, IsGitHubShorthand("gh:org/repo/path.yaml"))
	assert.True(t, IsGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml"))
}

func TestIsGitHubShorthand_False(t *testing.T) {
	t.Parallel()

	assert.False(t, IsGitHubShorthand("slack"))
	assert.False(t, IsGitHubShorthand("https://example.com"))
	assert.False(t, IsGitHubShorthand("/path/to/file.yaml"))
	assert.False(t, IsGitHubShorthand(""))
}

func TestParseGitHubShorthand_SimpleFormat(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/netresearch/ofelia-presets/main/slack.yaml", url)
}

func TestParseGitHubShorthand_WithVersion(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml@v1.0.0")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/netresearch/ofelia-presets/v1.0.0/slack.yaml", url)
}

func TestParseGitHubShorthand_WithBranch(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:netresearch/ofelia-presets/slack.yaml@develop")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/netresearch/ofelia-presets/develop/slack.yaml", url)
}

func TestParseGitHubShorthand_NestedPath(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:org/repo/notifications/slack.yaml")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/org/repo/main/notifications/slack.yaml", url)
}

func TestParseGitHubShorthand_AutoAddYAML(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:org/repo/slack")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/org/repo/main/slack.yaml", url)
}

func TestParseGitHubShorthand_YMLExtension(t *testing.T) {
	t.Parallel()

	url, err := ParseGitHubShorthand("gh:org/repo/slack.yml")

	require.NoError(t, err)
	assert.Equal(t, "https://raw.githubusercontent.com/org/repo/main/slack.yml", url)
}

func TestParseGitHubShorthand_InvalidFormat(t *testing.T) {
	t.Parallel()

	_, err := ParseGitHubShorthand("gh:")

	assert.Error(t, err)
}

func TestParseGitHubShorthand_NotGitHub(t *testing.T) {
	t.Parallel()

	_, err := ParseGitHubShorthand("https://example.com")

	assert.Error(t, err)
}

func TestParseGitHubShorthandDetails(t *testing.T) {
	t.Parallel()

	gh, err := ParseGitHubShorthandDetails("gh:netresearch/ofelia-presets/notifications/slack.yaml@v1.0.0")

	require.NoError(t, err)
	assert.NotNil(t, gh)
	assert.Equal(t, "netresearch", gh.Org)
	assert.Equal(t, "ofelia-presets", gh.Repo)
	assert.Equal(t, "notifications/slack.yaml", gh.Path)
	assert.Equal(t, "v1.0.0", gh.Version)
}

func TestParseGitHubShorthandDetails_DefaultVersion(t *testing.T) {
	t.Parallel()

	gh, err := ParseGitHubShorthandDetails("gh:org/repo/path.yaml")

	require.NoError(t, err)
	assert.Equal(t, "main", gh.Version)
}

func TestIsVersioned_True(t *testing.T) {
	t.Parallel()

	assert.True(t, IsVersioned("gh:org/repo/path@v1.0.0"))
	assert.True(t, IsVersioned("gh:org/repo/path@main"))
}

func TestIsVersioned_False(t *testing.T) {
	t.Parallel()

	assert.False(t, IsVersioned("gh:org/repo/path"))
	assert.False(t, IsVersioned("slack"))
}

func TestFormatGitHubShorthand(t *testing.T) {
	t.Parallel()

	shorthand := FormatGitHubShorthand("netresearch", "ofelia-presets", "slack.yaml", "v1.0.0")
	assert.Equal(t, "gh:netresearch/ofelia-presets/slack.yaml@v1.0.0", shorthand)
}

func TestFormatGitHubShorthand_DefaultVersion(t *testing.T) {
	t.Parallel()

	shorthand := FormatGitHubShorthand("netresearch", "ofelia-presets", "slack.yaml", "main")
	assert.Equal(t, "gh:netresearch/ofelia-presets/slack.yaml", shorthand)
}

func TestFormatGitHubShorthand_NoPath(t *testing.T) {
	t.Parallel()

	shorthand := FormatGitHubShorthand("org", "repo", "", "v1.0.0")
	assert.Equal(t, "gh:org/repo@v1.0.0", shorthand)
}

func TestExtractVersionFromShorthand(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "v1.0.0", ExtractVersionFromShorthand("gh:org/repo/path@v1.0.0"))
	assert.Equal(t, "main", ExtractVersionFromShorthand("gh:org/repo/path@main"))
	assert.Empty(t, ExtractVersionFromShorthand("gh:org/repo/path"))
}

func TestStripVersionFromShorthand(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "gh:org/repo/path", StripVersionFromShorthand("gh:org/repo/path@v1.0.0"))
	assert.Equal(t, "gh:org/repo/path", StripVersionFromShorthand("gh:org/repo/path"))
}

func TestIsSemanticVersion(t *testing.T) {
	t.Parallel()

	assert.True(t, IsSemanticVersion("v1.0.0"))
	assert.True(t, IsSemanticVersion("1.0.0"))
	assert.True(t, IsSemanticVersion("v2.3.4"))
	assert.False(t, IsSemanticVersion("main"))
	assert.False(t, IsSemanticVersion("develop"))
	assert.False(t, IsSemanticVersion("feature/test"))
}

func TestIsBranch(t *testing.T) {
	t.Parallel()

	assert.True(t, IsBranch("main"))
	assert.True(t, IsBranch("master"))
	assert.True(t, IsBranch("develop"))
	assert.True(t, IsBranch("feature/test"))
	assert.True(t, IsBranch("fix/bug"))
	assert.True(t, IsBranch("release/1.0"))
	assert.False(t, IsBranch("v1.0.0"))
}

func TestValidateGitHubShorthand(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateGitHubShorthand("gh:org/repo/path.yaml"))
	require.NoError(t, ValidateGitHubShorthand("gh:org/repo/path.yaml@v1.0.0"))
	assert.Error(t, ValidateGitHubShorthand("slack"))
	assert.Error(t, ValidateGitHubShorthand("https://example.com"))
}

func TestGitHubShorthand_RoundTrip(t *testing.T) {
	t.Parallel()

	original := "gh:netresearch/ofelia-presets/slack.yaml@v1.0.0"
	details, err := ParseGitHubShorthandDetails(original)
	require.NoError(t, err)

	reconstructed := FormatGitHubShorthand(details.Org, details.Repo, details.Path, details.Version)
	assert.Equal(t, original, reconstructed)
}

func TestGitHubShorthand_URLGeneration(t *testing.T) {
	t.Parallel()

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
		require.NoError(t, err, "Failed to parse %s", tc.shorthand)
		assert.Equal(t, tc.expectedURL, url, "For %s", tc.shorthand)
	}
}
