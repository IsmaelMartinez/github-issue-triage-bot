package comment

import (
	"regexp"
	"strings"
)

var (
	gfmImageRe        = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	dangerousLinkRe   = regexp.MustCompile(`\[(.*?)\]\((?i:javascript|data|vbscript):.*\)`)
	scriptTagRe       = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlTagRe         = regexp.MustCompile(`<[^>]*>`)
	dangerousSchemeRe = regexp.MustCompile(`(?i)^(javascript|data|vbscript):`)
)

func sanitizeLLMOutput(s string) string {
	s = gfmImageRe.ReplaceAllString(s, "")
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
