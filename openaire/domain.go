package openaire

import (
	"context"
	"fmt"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the openaire driver.
type Domain struct{}

// Info describes the scheme, hostnames, and binary identity.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "openaire",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "openaire",
			Short:  "A command line for OpenAIRE open science research.",
			Long: `A command line for the OpenAIRE public search API.

openaire reads publications, datasets, and software from api.openaire.eu
over HTTPS, shapes them into clean records, and prints output that pipes
into the rest of your tools. No API key required.`,
			Site: Host,
			Repo: "https://github.com/tamnd/openaire-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search publications, datasets, or software",
		Args:    []kit.Arg{{Name: "keywords", Help: "search terms"}}}, searchPubs)

	kit.Handle(app, kit.OpMeta{Name: "publications", Group: "read", List: true,
		Summary: "Search publications (shortcut for search --type publications)",
		Args:    []kit.Arg{{Name: "keywords", Help: "search terms"}}}, searchPublications)

	kit.Handle(app, kit.OpMeta{Name: "datasets", Group: "read", List: true,
		Summary: "Search datasets (shortcut for search --type datasets)",
		Args:    []kit.Arg{{Name: "keywords", Help: "search terms"}}}, searchDatasets)
}

// newClient builds the Client from kit config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- input structs ---

type searchInput struct {
	Keywords string  `kit:"arg" help:"search terms"`
	Type     string  `kit:"flag" help:"publications|datasets|software (default: publications)"`
	Page     int     `kit:"flag,inherit" help:"page number (default 1)"`
	Size     int     `kit:"flag,inherit" help:"results per page (default 25)"`
	Client   *Client `kit:"inject"`
}

type keywordsInput struct {
	Keywords string  `kit:"arg" help:"search terms"`
	Page     int     `kit:"flag,inherit" help:"page number (default 1)"`
	Size     int     `kit:"flag,inherit" help:"results per page (default 25)"`
	Client   *Client `kit:"inject"`
}

// --- handlers ---

func searchPubs(ctx context.Context, in searchInput, emit func(*Publication) error) error {
	kind := in.Type
	if kind == "" {
		kind = "publications"
	}
	items, err := in.Client.Search(ctx, kind, in.Keywords, in.Page, in.Size)
	if err != nil {
		return err
	}
	for i := range items {
		if err := emit(&items[i]); err != nil {
			return err
		}
	}
	return nil
}

func searchPublications(ctx context.Context, in keywordsInput, emit func(*Publication) error) error {
	items, err := in.Client.Search(ctx, "publications", in.Keywords, in.Page, in.Size)
	if err != nil {
		return err
	}
	for i := range items {
		if err := emit(&items[i]); err != nil {
			return err
		}
	}
	return nil
}

func searchDatasets(ctx context.Context, in keywordsInput, emit func(*Publication) error) error {
	items, err := in.Client.Search(ctx, "datasets", in.Keywords, in.Page, in.Size)
	if err != nil {
		return err
	}
	for i := range items {
		if err := emit(&items[i]); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns any accepted input into (type, id).
func (Domain) Classify(input string) (string, string, error) {
	if input == "" {
		return "", "", errs.Usage("openaire: empty reference")
	}
	return "publication", input, nil
}

// Locate returns the live https URL for a (type, id).
func (Domain) Locate(t, id string) (string, error) {
	switch t {
	case "publication":
		return fmt.Sprintf("https://explore.openaire.eu/search/result?id=%s", id), nil
	default:
		return "", errs.Usage("openaire has no resource type %q", t)
	}
}
