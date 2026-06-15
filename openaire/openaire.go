// Package openaire is the library behind the openaire command line:
// the HTTP client, request shaping, and typed data models for the OpenAIRE
// public search API (https://api.openaire.eu).
//
// No API key is required. The Client paces requests, sets a real User-Agent,
// and retries transient failures (429 and 5xx).
package openaire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Host is the API hostname.
const Host = "api.openaire.eu"

// Config holds all tunable parameters for the Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://api.openaire.eu",
		UserAgent: "openaire-cli/0.1 (+https://github.com/tamnd/openaire-cli)",
		Rate:      500 * time.Millisecond,
		Timeout:   30 * time.Second,
		Retries:   3,
	}
}

// Publication holds data for a single publication, dataset, or software record.
type Publication struct {
	ID        string `kit:"id" json:"id"`
	Title     string `json:"title"`
	Authors   string `json:"authors"`
	Date      string `json:"date"`
	Publisher string `json:"publisher"`
	Language  string `json:"language"`
	Access    string `json:"access"`
	Type      string `json:"type"`
	DOI       string `json:"doi,omitempty"`
}

// --- wire types for deeply-nested OpenAIRE JSON ---

type wireTextVal struct {
	Text string `json:"$"`
}

type wireAttrVal struct {
	ClassName string `json:"@classname"`
}

// wireCreators handles creator being either a single object or an array.
type wireCreators []wireTextVal

func (wc *wireCreators) UnmarshalJSON(data []byte) error {
	// try array first
	var arr []wireTextVal
	if err := json.Unmarshal(data, &arr); err == nil {
		*wc = arr
		return nil
	}
	// single object
	var single wireTextVal
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*wc = wireCreators{single}
	return nil
}

// wireTitles handles title being either a single object or an array.
type wireTitles []wireTextVal

func (wt *wireTitles) UnmarshalJSON(data []byte) error {
	var arr []wireTextVal
	if err := json.Unmarshal(data, &arr); err == nil {
		*wt = arr
		return nil
	}
	var single wireTextVal
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*wt = wireTitles{single}
	return nil
}

type wireResult struct {
	Header struct {
		ObjID wireTextVal `json:"dri:objIdentifier"`
	} `json:"header"`
	Metadata struct {
		OafEntity struct {
			OafResult struct {
				Title        wireTitles   `json:"title"`
				Creator      wireCreators `json:"creator"`
				DateAccepted wireTextVal  `json:"dateofacceptance"`
				Publisher    wireTextVal  `json:"publisher"`
				Language     wireAttrVal  `json:"language"`
				AccessRight  wireAttrVal  `json:"bestaccessright"`
				ResultType   wireAttrVal  `json:"resulttype"`
				Children     struct {
					Instance struct {
						AltID wireTextVal `json:"alternateidentifier"`
					} `json:"instance"`
				} `json:"children"`
			} `json:"oaf:result"`
		} `json:"oaf:entity"`
	} `json:"metadata"`
}

type wireResponse struct {
	Response struct {
		Header struct {
			Total wireTextVal `json:"total"`
			Page  wireTextVal `json:"page"`
			Size  wireTextVal `json:"size"`
		} `json:"header"`
		Results struct {
			Result []wireResult `json:"result"`
		} `json:"results"`
	} `json:"response"`
}

// Client talks to the OpenAIRE API.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client with the given configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Search searches publications, datasets, or software.
// kind must be "publications", "datasets", or "software".
func (c *Client) Search(ctx context.Context, kind, keywords string, page, size int) ([]Publication, error) {
	if kind == "" {
		kind = "publications"
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 25
	}

	params := url.Values{}
	params.Set("format", "json")
	params.Set("page", fmt.Sprintf("%d", page))
	params.Set("size", fmt.Sprintf("%d", size))
	params.Set("keywords", keywords)

	rawURL := c.cfg.BaseURL + "/search/" + kind + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var resp wireResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	results := resp.Response.Results.Result
	out := make([]Publication, 0, len(results))
	for _, r := range results {
		out = append(out, flattenPublication(r))
	}
	return out, nil
}

func flattenPublication(r wireResult) Publication {
	res := r.Metadata.OafEntity.OafResult

	title := ""
	if len(res.Title) > 0 {
		title = res.Title[0].Text
	}

	authors := make([]string, 0, len(res.Creator))
	for _, c := range res.Creator {
		if c.Text != "" {
			authors = append(authors, c.Text)
		}
	}

	return Publication{
		ID:        r.Header.ObjID.Text,
		Title:     title,
		Authors:   strings.Join(authors, ", "),
		Date:      res.DateAccepted.Text,
		Publisher: res.Publisher.Text,
		Language:  res.Language.ClassName,
		Access:    res.AccessRight.ClassName,
		Type:      res.ResultType.ClassName,
		DOI:       res.Children.Instance.AltID.Text,
	}
}

// get fetches a URL and returns the body, pacing and retrying as configured.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
