package meraki

import (
	"net/http"
	"testing"
)

func TestNextLinkSingleRelNext(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Add("Link", `<https://api.meraki.com/api/v1/foo?startingAfter=abc>; rel="next"`)

	got := nextLink(h)
	want := "https://api.meraki.com/api/v1/foo?startingAfter=abc"
	if got != want {
		t.Fatalf("nextLink=%q, want %q", got, want)
	}
}

func TestNextLinkMultipleLinkHeaders(t *testing.T) {
	t.Parallel()

	// Two separate Link headers — only the second has rel=next.
	h := http.Header{}
	h.Add("Link", `<https://api.meraki.com/api/v1/foo?rel=prev>; rel="prev"`)
	h.Add("Link", `<https://api.meraki.com/api/v1/foo?startingAfter=xyz>; rel="next"`)

	got := nextLink(h)
	want := "https://api.meraki.com/api/v1/foo?startingAfter=xyz"
	if got != want {
		t.Fatalf("nextLink=%q, want %q", got, want)
	}
}

func TestNextLinkPrevAndNextInSameHeader(t *testing.T) {
	t.Parallel()

	// A single Link header with comma-separated entries that include both prev and next.
	h := http.Header{}
	h.Set("Link", `<https://api.meraki.com/api/v1/foo?before=1>; rel="prev", <https://api.meraki.com/api/v1/foo?after=2>; rel="next"`)

	got := nextLink(h)
	want := "https://api.meraki.com/api/v1/foo?after=2"
	if got != want {
		t.Fatalf("nextLink=%q, want %q", got, want)
	}
}

func TestNextLinkQuotedRel(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Link", `<https://example.com/next>; rel="next"`)
	if got := nextLink(h); got != "https://example.com/next" {
		t.Fatalf("quoted rel: got %q", got)
	}
}

func TestNextLinkUnquotedRel(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Link", `<https://example.com/next>; rel=next`)
	if got := nextLink(h); got != "https://example.com/next" {
		t.Fatalf("unquoted rel: got %q", got)
	}
}

func TestNextLinkAbsentHeader(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	if got := nextLink(h); got != "" {
		t.Fatalf("no header: got %q, want empty", got)
	}
}

func TestNextLinkMalformed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{"no angle brackets", `https://example.com; rel="next"`},
		{"no rel attribute", `<https://example.com>`},
		{"rel is prev only", `<https://example.com>; rel="prev"`},
		{"junk value", `not a link header at all`},
		{"empty string", ``},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			h.Set("Link", tc.header)
			if got := nextLink(h); got != "" {
				t.Fatalf("malformed header %q: got %q, want empty", tc.header, got)
			}
		})
	}
}
