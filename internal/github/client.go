package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"

	gogithub "github.com/google/go-github/v69/github"
)

type Client struct {
	gh            *gogithub.Client
	log           io.Writer
	authenticated bool
	apiCalls      atomic.Int64
	rateRemaining int
}

// NewClient creates a GitHub client. Token resolution order:
// 1. token argument (from config.yaml via viper)
// 2. GITHUB_TOKEN environment variable
// 3. unauthenticated (60 req/hr)
func NewClient(token string, log io.Writer) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	var gh *gogithub.Client
	var authenticated bool

	if token != "" {
		gh = gogithub.NewClient(nil).WithAuthToken(token)
		authenticated = true
		fmt.Fprintln(log, "[auth] Authenticated — 5,000 requests/hour")
	} else {
		gh = gogithub.NewClient(nil)
		fmt.Fprintln(log, "[auth] No token — unauthenticated mode (60 requests/hour)")
	}

	return &Client{
		gh:            gh,
		log:           log,
		authenticated: authenticated,
	}
}

func (c *Client) trackCall(resp *gogithub.Response) {
	c.apiCalls.Add(1)
	if resp != nil {
		c.rateRemaining = resp.Rate.Remaining
	}
}

func (c *Client) checkRateLimit(resp *gogithub.Response) error {
	if resp != nil && resp.Rate.Remaining == 0 {
		return fmt.Errorf("GitHub API rate limit exhausted (resets at %v). Set GITHUB_TOKEN to get 5,000 req/hr", resp.Rate.Reset.Time)
	}
	if resp != nil && resp.Rate.Remaining < 10 {
		fmt.Fprintf(c.log, "[rate-limit] WARNING: only %d API calls remaining (resets at %v)\n", resp.Rate.Remaining, resp.Rate.Reset.Time)
	}
	return nil
}

func (c *Client) Log() io.Writer {
	return c.log
}

func (c *Client) APICallsMade() int {
	return int(c.apiCalls.Load())
}

func (c *Client) RateRemaining() int {
	return c.rateRemaining
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*gogithub.PullRequest, *gogithub.Response, error) {
	pr, resp, err := c.gh.PullRequests.Get(ctx, owner, repo, number)
	if resp != nil {
		c.trackCall(resp)
		if rateErr := c.checkRateLimit(resp); rateErr != nil {
			return nil, resp, rateErr
		}
	}
	return pr, resp, err
}

func (c *Client) ListPullRequests(ctx context.Context, owner, repo string, opts *gogithub.PullRequestListOptions) ([]*gogithub.PullRequest, *gogithub.Response, error) {
	prs, resp, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
	if resp != nil {
		c.trackCall(resp)
		if rateErr := c.checkRateLimit(resp); rateErr != nil {
			return nil, resp, rateErr
		}
	}
	return prs, resp, err
}

func (c *Client) ListPRFiles(ctx context.Context, owner, repo string, prNumber int, opts *gogithub.ListOptions) ([]*gogithub.CommitFile, *gogithub.Response, error) {
	files, resp, err := c.gh.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
	if resp != nil {
		c.trackCall(resp)
		if rateErr := c.checkRateLimit(resp); rateErr != nil {
			return nil, resp, rateErr
		}
	}
	return files, resp, err
}

func (c *Client) ListReviews(ctx context.Context, owner, repo string, prNumber int, opts *gogithub.ListOptions) ([]*gogithub.PullRequestReview, *gogithub.Response, error) {
	reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, prNumber, opts)
	if resp != nil {
		c.trackCall(resp)
		if rateErr := c.checkRateLimit(resp); rateErr != nil {
			return nil, resp, rateErr
		}
	}
	return reviews, resp, err
}

func (c *Client) SetTransport(transport http.RoundTripper) {
	c.gh.Client().Transport = transport
}
