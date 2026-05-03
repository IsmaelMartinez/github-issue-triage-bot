package comment

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	gfmImageRe        = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)       // ![alt](url)
	gfmRefImageRe     = regexp.MustCompile(`!\[([^\]]*)\]\[[^\]]+\]`)      // ![alt][ref]
	gfmRefDefRe       = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s+\S+.*$`) // [ref]: url (link definition)
	dangerousLinkRe   = regexp.MustCompile(`\[(.*?)\]\((?i:javascript|data|vbscript):.*\)`)
	scriptTagRe       = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlTagRe         = regexp.MustCompile(`<[^>]*>`)
	dangerousSchemeRe = regexp.MustCompile(`(?i)^(javascript|data|vbscript):`)

	// upstreamURLRe matches GitHub issue/PR URLs and captures owner, repo, and number.
	upstreamURLRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/(?:issues|pull)/(\d+)`)
)

// UpstreamRef identifies an upstream GitHub repository issue/PR reference parsed
// from a doc_url. It is used by rewriteBareUpstreamRefs to qualify bare `#NNN`
// references in synthesised prose.
type UpstreamRef struct {
	Owner  string
	Repo   string
	Number int
}

// SanitizeLLMOutput strips images, dangerous links, script tags, and HTML from LLM text.
func SanitizeLLMOutput(s string) string {
	return sanitizeLLMOutput(s)
}

func sanitizeLLMOutput(s string) string {
	s = gfmImageRe.ReplaceAllString(s, "")
	s = gfmRefImageRe.ReplaceAllString(s, "")
	s = gfmRefDefRe.ReplaceAllString(s, "")
	s = dangerousLinkRe.ReplaceAllString(s, "[$1](removed)")
	s = scriptTagRe.ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	return s
}

func sanitizeURL(u string) string {
	trimmed := strings.TrimSpace(u)
	if dangerousSchemeRe.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

// ExtractUpstreamRefs parses GitHub issue/PR URLs out of the given doc URLs and
// returns the matching UpstreamRef set. URLs that do not point to a GitHub
// issue or pull request are skipped.
func ExtractUpstreamRefs(docURLs []string) []UpstreamRef {
	var refs []UpstreamRef
	for _, u := range docURLs {
		m := upstreamURLRe.FindStringSubmatch(strings.TrimSpace(u))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}
		refs = append(refs, UpstreamRef{Owner: m[1], Repo: m[2], Number: n})
	}
	return refs
}

// RewriteBareUpstreamRefs rewrites bare `#NNN` references in text to the
// qualified `OWNER/REPO#NNN` form when the number matches an upstream
// reference. It does NOT touch numbers inside markdown link text/URLs,
// already-qualified `owner/repo#NNN` refs, or full URLs. When a number
// appears in localIssueHints, the rewrite is skipped (local takes precedence
// over upstream — bare `#NNN` in maintainer prose conventionally means the
// host repo). The rewrite is idempotent.
func RewriteBareUpstreamRefs(text string, upstreamRefs []UpstreamRef, localIssueHints map[int]bool) string {
	return rewriteBareUpstreamRefs(text, upstreamRefs, localIssueHints)
}

func rewriteBareUpstreamRefs(text string, upstreamRefs []UpstreamRef, localIssueHints map[int]bool) string {
	if len(upstreamRefs) == 0 {
		return text
	}

	// Index upstream refs by number. If two upstream refs share the same
	// number (unlikely in practice), the first wins.
	byNumber := make(map[int]UpstreamRef, len(upstreamRefs))
	for _, r := range upstreamRefs {
		if _, exists := byNumber[r.Number]; !exists {
			byNumber[r.Number] = r
		}
	}

	// Walk the text, tracking exclusion zones where rewrites must not happen:
	// inside markdown link text/URLs `[...](...)` and inside full URLs
	// (http://, https://). Already-qualified `owner/repo#NNN` is handled by
	// the lookbehind check on the byte immediately before `#`.
	var out strings.Builder
	out.Grow(len(text))

	i := 0
	for i < len(text) {
		ch := text[i]

		// Skip markdown link `[text](url)` as one opaque span.
		if ch == '[' {
			if end := findMarkdownLinkEnd(text, i); end > i {
				out.WriteString(text[i:end])
				i = end
				continue
			}
		}

		// Skip bare URLs (http:// or https://) as opaque spans so a `#NNN`
		// inside a URL fragment or path is never rewritten.
		if ch == 'h' || ch == 'H' {
			if end := findURLEnd(text, i); end > i {
				out.WriteString(text[i:end])
				i = end
				continue
			}
		}

		// Detect `#NNN` (bare issue reference).
		if ch == '#' && i+1 < len(text) && isDigit(text[i+1]) {
			// Already-qualified refs like `owner/repo#NNN` have an alphanumeric,
			// `-`, `_`, `.`, or `/` immediately before the `#`. Treat any such
			// preceding character as "qualified" and skip rewriting.
			if i > 0 && isQualifierByte(text[i-1]) {
				out.WriteByte(ch)
				i++
				continue
			}

			// Read the number.
			j := i + 1
			for j < len(text) && isDigit(text[j]) {
				j++
			}
			numStr := text[i+1 : j]
			n, err := strconv.Atoi(numStr)
			if err != nil {
				out.WriteString(text[i:j])
				i = j
				continue
			}

			// Local hints win over upstream ambiguity.
			if localIssueHints[n] {
				out.WriteString(text[i:j])
				i = j
				continue
			}

			ref, ok := byNumber[n]
			if !ok {
				out.WriteString(text[i:j])
				i = j
				continue
			}

			out.WriteString(ref.Owner)
			out.WriteByte('/')
			out.WriteString(ref.Repo)
			out.WriteByte('#')
			out.WriteString(numStr)
			i = j
			continue
		}

		out.WriteByte(ch)
		i++
	}
	return out.String()
}

// findMarkdownLinkEnd returns the byte offset just past a `[text](url)` span
// starting at i, or i if no complete span is present. Nested brackets inside
// link text are not handled (rare, and the worst case is we treat them as
// non-link prose, which is safe).
func findMarkdownLinkEnd(s string, i int) int {
	if i >= len(s) || s[i] != '[' {
		return i
	}
	closeBracket := strings.IndexByte(s[i+1:], ']')
	if closeBracket < 0 {
		return i
	}
	closeBracket += i + 1
	if closeBracket+1 >= len(s) || s[closeBracket+1] != '(' {
		return i
	}
	closeParen := strings.IndexByte(s[closeBracket+2:], ')')
	if closeParen < 0 {
		return i
	}
	return closeBracket + 2 + closeParen + 1
}

// findURLEnd returns the byte offset just past an `http://` or `https://`
// URL starting at i, or i if no URL begins there. URL termination is at the
// first whitespace, closing bracket/paren, or end-of-string.
func findURLEnd(s string, i int) int {
	rest := s[i:]
	if !strings.HasPrefix(strings.ToLower(rest), "http://") && !strings.HasPrefix(strings.ToLower(rest), "https://") {
		return i
	}
	end := i
	for end < len(s) {
		c := s[end]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ')' || c == ']' || c == '>' {
			break
		}
		end++
	}
	return end
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isQualifierByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.' || b == '/'
}
