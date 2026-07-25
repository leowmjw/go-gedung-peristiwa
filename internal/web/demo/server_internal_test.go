package demo

import (
	"testing"
)

func TestAgencyGroup(t *testing.T) {
	cases := map[string]string{
		"ktmb":                    "ktmb",
		"prasarana-rapid-bus-kl":  "prasarana",
		"mybas-ipoh":              "mybas",
		"other":                   "mybas",
	}
	for agency, want := range cases {
		if got := agencyGroup(agency); got != want {
			t.Fatalf("agencyGroup(%q) = %q, want %q", agency, got, want)
		}
	}
}

func TestMustJSON(t *testing.T) {
	s := mustJSON(map[string]any{"a": 1})
	if s == "" || s == "{}" {
		t.Fatalf("mustJSON = %q", s)
	}
}
