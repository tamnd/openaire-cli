package openaire

import (
	"strings"
	"testing"
)

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "openaire" {
		t.Errorf("Scheme = %q, want openaire", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "openaire" {
		t.Errorf("Identity.Binary = %q, want openaire", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	typ, id, err := Domain{}.Classify("some-openaire-id")
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}
	if typ != "publication" {
		t.Errorf("type = %q, want publication", typ)
	}
	if id != "some-openaire-id" {
		t.Errorf("id = %q, want some-openaire-id", id)
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return error")
	}
}

func TestLocatePublication(t *testing.T) {
	got, err := Domain{}.Locate("publication", "dedup_wf_001::abc123")
	if err != nil {
		t.Fatalf("Locate error: %v", err)
	}
	if !strings.Contains(got, "explore.openaire.eu") {
		t.Errorf("Locate = %q, want URL on explore.openaire.eu", got)
	}
	if !strings.Contains(got, "dedup_wf_001::abc123") {
		t.Errorf("Locate = %q, want it to contain the id", got)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "id")
	if err == nil {
		t.Error("Locate with unknown type should return error")
	}
}
