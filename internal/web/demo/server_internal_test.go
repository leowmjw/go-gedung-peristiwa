package demo

import (
	"testing"
)

func TestAgencyGroup(t *testing.T) {
	cases := map[string]string{
		"ktmb":                   "ktmb",
		"prasarana-rapid-bus-kl": "prasarana",
		"mybas-ipoh":             "mybas",
		"other":                  "mybas",
	}
	for agency, want := range cases {
		if got := agencyGroup(agency); got != want {
			t.Fatalf("agencyGroup(%q) = %q, want %q", agency, got, want)
		}
	}
}

func TestMustJSON(t *testing.T) {
	got := string(mustJSON("klang-valley"))
	if got != `"klang-valley"` {
		t.Fatalf("mustJSON string = %q, want quoted JSON without extra escapes", got)
	}
	got = string(mustJSON(10))
	if got != "10" {
		t.Fatalf("mustJSON int = %q, want 10", got)
	}
}
