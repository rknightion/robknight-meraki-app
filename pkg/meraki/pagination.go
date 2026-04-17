package meraki

import (
	"net/http"
	"regexp"
	"strings"
)

// nextLinkRe extracts the URL of a `rel="next"` Link header entry.
//   Link: <https://api.meraki.com/api/v1/foo?startingAfter=abc>; rel=next
var nextLinkRe = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

// MaxPages caps recursive pagination. When this limit is hit the paged call will return the
// accumulated result and annotate the response as truncated.
const MaxPages = 100

// nextLink returns the URL advertised by the `rel="next"` Link header, or "" if none.
// Handles multiple rel values and quoted or unquoted rel attributes.
func nextLink(h http.Header) string {
	for _, v := range h.Values("Link") {
		for part := range strings.SplitSeq(v, ",") {
			if match := nextLinkRe.FindStringSubmatch(strings.TrimSpace(part)); len(match) == 2 {
				return match[1]
			}
		}
	}
	return ""
}
