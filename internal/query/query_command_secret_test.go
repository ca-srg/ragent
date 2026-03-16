package query

import "testing"

func TestParseFilters_StripsSecretFilter(t *testing.T) {
	filters, err := ParseFilters(`{"team":"search","secret":"true","SeCrEt":"false"}`)
	if err != nil {
		t.Fatalf("ParseFilters returned error: %v", err)
	}

	if _, ok := filters["secret"]; ok {
		t.Fatalf("secret filter should be removed")
	}
	if got := filters["team"]; got != "search" {
		t.Fatalf("expected team filter to remain, got %q", got)
	}
}

func TestParseFilters_EmptyInput(t *testing.T) {
	filters, err := ParseFilters("")
	if err != nil {
		t.Fatalf("ParseFilters returned error: %v", err)
	}
	if filters != nil {
		t.Fatalf("expected nil filters for empty input")
	}
}
