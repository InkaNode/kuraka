// Package releases reads the latest release tag of public GitHub repositories.
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DefaultBaseURL is the GitHub REST API. Unauthenticated calls allow 60
// requests per hour.
const DefaultBaseURL = "https://api.github.com"

const maxBody = 1 << 20

// Tags are shown in the panel and written to files; anything else is refused.
var validTag = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,99}$`)
var validCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Release identifies the latest release and its immutable commit, if provided.
type Release struct {
	Tag, Commit, URL string
}

// Client reads GitHub releases. The zero value uses DefaultBaseURL and
// http.DefaultClient.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// Latest returns the tag of the latest release of repo ("owner/name").
func (c *Client) Latest(ctx context.Context, repo string) (string, error) {
	release, err := c.LatestRelease(ctx, repo)
	return release.Tag, err
}

// LatestRelease returns the latest release of repo ("owner/name").
func (c *Client) LatestRelease(ctx context.Context, repo string) (Release, error) {
	base, client := c.BaseURL, c.HTTP
	if base == "" {
		base = DefaultBaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kuraka")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("releases: %s: %w", repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("releases: %s: http %d", repo, resp.StatusCode)
	}
	var body struct {
		TagName         string `json:"tag_name"`
		TargetCommitish string `json:"target_commitish"`
		HTMLURL         string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("releases: %s: decode: %w", repo, err)
	}
	if !validTag.MatchString(body.TagName) {
		return Release{}, fmt.Errorf("releases: %s: invalid tag", repo)
	}
	release := Release{Tag: body.TagName}
	if validCommit.MatchString(body.TargetCommitish) {
		release.Commit = body.TargetCommitish
	}
	if strings.HasPrefix(body.HTMLURL, "https://github.com/") {
		release.URL = body.HTMLURL
	}
	return release, nil
}
