package comment

import "regexp"

var (
	dangerousLinkRe = regexp.MustCompile(`\[(.*?)\]\((?i:javascript|data|vbscript):.*\)`)
	scriptTagRe     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	htmlTagRe       = regexp.MustCompile(`<[^>]*>`)
)

func sanitizeLLMOutput(s string) string {
	s = dangerousLinkRe.ReplaceAllString(s, "[$1](removed)")
	s = scriptTagRe.ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	return s
}
