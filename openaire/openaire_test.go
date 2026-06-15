package openaire_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/openaire-cli/openaire"
)

func newTestClient(ts *httptest.Server) *openaire.Client {
	cfg := openaire.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	return openaire.NewClient(cfg)
}

// buildResponse returns a raw JSON response body for the given kind and results.
func buildResponse(results []map[string]any) []byte {
	resp := map[string]any{
		"response": map[string]any{
			"header": map[string]any{
				"total": map[string]any{"$": "100"},
				"page":  map[string]any{"$": "1"},
				"size":  map[string]any{"$": "25"},
			},
			"results": map[string]any{
				"result": results,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func makeResult(id, title, author, date, publisher, lang, access, rtype, doi string) map[string]any {
	return map[string]any{
		"header": map[string]any{
			"dri:objIdentifier": map[string]any{"$": id},
		},
		"metadata": map[string]any{
			"oaf:entity": map[string]any{
				"oaf:result": map[string]any{
					"title":            []any{map[string]any{"$": title}},
					"creator":          []any{map[string]any{"$": author}},
					"dateofacceptance": map[string]any{"$": date},
					"publisher":        map[string]any{"$": publisher},
					"language":         map[string]any{"@classname": lang},
					"bestaccessright":  map[string]any{"@classname": access},
					"resulttype":       map[string]any{"@classname": rtype},
					"children": map[string]any{
						"instance": map[string]any{
							"alternateidentifier": map[string]any{"$": doi},
						},
					},
				},
			},
		},
	}
}

// TestSearchPublications checks that a search against /search/publications returns results.
func TestSearchPublications(t *testing.T) {
	r1 := makeResult("id1", "Machine Learning in Biology", "Smith, J.", "2023-05-10",
		"Nature", "English", "Open Access", "publication", "10.1000/xyz")
	r2 := makeResult("id2", "Deep Learning Survey", "Doe, A.", "2022-01-15",
		"Science", "English", "Restricted", "publication", "")

	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildResponse([]map[string]any{r1, r2}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	pubs, err := c.Search(context.Background(), "publications", "machine learning", 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/search/publications") {
		t.Errorf("path = %q, want to end with /search/publications", gotPath)
	}
	if len(pubs) != 2 {
		t.Fatalf("got %d results, want 2", len(pubs))
	}
	if pubs[0].ID != "id1" {
		t.Errorf("ID = %q, want id1", pubs[0].ID)
	}
	if pubs[0].Title != "Machine Learning in Biology" {
		t.Errorf("Title = %q", pubs[0].Title)
	}
	if pubs[0].Authors != "Smith, J." {
		t.Errorf("Authors = %q", pubs[0].Authors)
	}
	if pubs[0].DOI != "10.1000/xyz" {
		t.Errorf("DOI = %q", pubs[0].DOI)
	}
	if pubs[0].Access != "Open Access" {
		t.Errorf("Access = %q", pubs[0].Access)
	}
}

// TestSearchDatasets checks that the /search/datasets endpoint is used when kind=datasets.
func TestSearchDatasets(t *testing.T) {
	r1 := makeResult("ds1", "Climate Dataset 2020", "Green, P.", "2020-03-01",
		"CERN", "English", "Open Access", "dataset", "")

	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildResponse([]map[string]any{r1}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	pubs, err := c.Search(context.Background(), "datasets", "climate", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/search/datasets") {
		t.Errorf("path = %q, want to end with /search/datasets", gotPath)
	}
	if len(pubs) != 1 {
		t.Fatalf("got %d results, want 1", len(pubs))
	}
	if pubs[0].ID != "ds1" {
		t.Errorf("ID = %q, want ds1", pubs[0].ID)
	}
}

// TestEmptyResults checks that an empty result array returns an empty slice with no error.
func TestEmptyResults(t *testing.T) {
	resp := map[string]any{
		"response": map[string]any{
			"header": map[string]any{
				"total": map[string]any{"$": "0"},
				"page":  map[string]any{"$": "1"},
				"size":  map[string]any{"$": "25"},
			},
			"results": map[string]any{
				"result": []any{},
			},
		},
	}
	b, _ := json.Marshal(resp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	pubs, err := c.Search(context.Background(), "publications", "noresults", 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 0 {
		t.Errorf("got %d results, want 0", len(pubs))
	}
}

// TestRetryOn503 checks that the client retries on 503 and succeeds on the second attempt.
func TestRetryOn503(t *testing.T) {
	r1 := makeResult("id1", "Retry Test Publication", "Lee, K.", "2021-06-01",
		"MIT Press", "English", "Open Access", "publication", "")

	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildResponse([]map[string]any{r1}))
	}))
	defer ts.Close()

	cfg := openaire.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	cfg.Retries = 3
	c := openaire.NewClient(cfg)

	start := time.Now()
	pubs, err := c.Search(context.Background(), "publications", "retry", 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("server saw %d hits, want 2", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
	if len(pubs) != 1 {
		t.Errorf("got %d results, want 1", len(pubs))
	}
}

// TestUserAgent checks that every request carries openaire-cli in User-Agent.
func TestUserAgent(t *testing.T) {
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildResponse(nil))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, _ = c.Search(context.Background(), "publications", "test", 1, 25)

	if !strings.Contains(gotUA, "openaire-cli") {
		t.Errorf("User-Agent = %q, want it to contain openaire-cli", gotUA)
	}
}
