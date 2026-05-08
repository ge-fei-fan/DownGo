package app

import "testing"

func TestBuildAccessEntriesForWildcardHost(t *testing.T) {
	t.Parallel()

	entries := buildAccessEntries("0.0.0.0", 12225, []string{"192.168.1.10", "127.0.0.1", "10.0.0.5"})
	if len(entries) != 3 {
		t.Fatalf("expected 3 unique entries, got %d", len(entries))
	}
	if entries[0].Host != "127.0.0.1" {
		t.Fatalf("expected localhost first, got %+v", entries[0])
	}
	if entries[1].Host != "10.0.0.5" || entries[2].Host != "192.168.1.10" {
		t.Fatalf("unexpected sort order: %+v", entries)
	}
}

func TestBuildAccessEntriesForSpecificHost(t *testing.T) {
	t.Parallel()

	entries := buildAccessEntries("192.168.1.20", 12225, []string{"10.0.0.5"})
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if entries[0].URL != "http://192.168.1.20:12225" {
		t.Fatalf("unexpected url: %+v", entries[0])
	}
}
