package db

import "testing"

const testSearchText = "ai news security update"

func TestNormalizeDiscoveryKeywords(t *testing.T) {
	input := []string{" AI ", "ai", "", "Security", "security"}
	got := NormalizeDiscoveryKeywords(input)

	want := []string{"ai", "security"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeDiscoveryKeywords() length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeDiscoveryKeywords()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEvaluateDiscoveryKeywords(t *testing.T) {
	discovery := DiscoveredChannel{
		Title:       "AI News",
		Description: "Security update",
	}

	allow := NormalizeDiscoveryKeywords([]string{"ai", "security"})
	deny := NormalizeDiscoveryKeywords([]string{"crypto"})

	allowMatch, denyMatch, text := EvaluateDiscoveryKeywords(discovery, allow, deny)

	if text != testSearchText {
		t.Fatalf("DiscoverySearchText = %q, want %q", text, testSearchText)
	}

	if !allowMatch {
		t.Fatal("allowMatch = false, want true")
	}

	if denyMatch {
		t.Fatal("denyMatch = true, want false")
	}
}

func TestFilterDiscoveriesByKeywords(t *testing.T) {
	discoveries := []DiscoveredChannel{
		{Title: "AI News", Description: "Security update"},
		{Title: "Crypto deals"},
		{Title: "", Description: ""},
	}

	allow := NormalizeDiscoveryKeywords([]string{"ai", "security"})
	deny := NormalizeDiscoveryKeywords([]string{"crypto"})

	filtered, allowMiss, denyHit := FilterDiscoveriesByKeywords(discoveries, allow, deny)

	if len(filtered) != 1 {
		t.Fatalf("filtered length = %d, want 1", len(filtered))
	}

	if allowMiss != 1 {
		t.Fatalf("allowMiss = %d, want 1", allowMiss)
	}

	if denyHit != 1 {
		t.Fatalf("denyHit = %d, want 1", denyHit)
	}
}
