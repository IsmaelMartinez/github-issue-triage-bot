package hats

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	h2Re           = regexp.MustCompile(`^## (\S.*)$`)
	issueRefRe     = regexp.MustCompile(`#(\d+)`)
	labelsLineRe   = regexp.MustCompile(`(?i)labels:\s*([^.]+)\.?`)
	keywordsLineRe = regexp.MustCompile(`(?i)keywords:\s*([^.]+)\.?`)
)

// Parse converts hats.md content into a Taxonomy.
func Parse(data []byte) (Taxonomy, error) {
	var tax Taxonomy
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var current *Hat
	var preambleLines []string
	var bodyLines []string
	flush := func() {
		if current == nil {
			return
		}
		applyBody(current, strings.Join(bodyLines, "\n"))
		tax.Hats = append(tax.Hats, *current)
		current = nil
		bodyLines = bodyLines[:0]
	}

	for scanner.Scan() {
		line := scanner.Text()
		if m := h2Re.FindStringSubmatch(line); m != nil {
			flush()
			current = &Hat{Name: strings.TrimSpace(m[1])}
			continue
		}
		if current == nil {
			preambleLines = append(preambleLines, line)
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return Taxonomy{}, fmt.Errorf("scan: %w", err)
	}
	if len(tax.Hats) == 0 {
		return Taxonomy{}, errors.New("no hats found (expected level-2 headings)")
	}
	tax.Preamble = strings.TrimSpace(strings.Join(preambleLines, "\n"))
	return tax, nil
}

func applyBody(h *Hat, body string) {
	paras := splitParagraphs(body)
	for _, p := range paras {
		switch {
		case startsWithCase(p, "When to pick"):
			h.WhenToPick = stripLabel(p, "When to pick")
		case startsWithCase(p, "Retrieval filter"):
			content := stripLabel(p, "Retrieval filter")
			h.RetrievalLabels = extractList(content, labelsLineRe)
			h.RetrievalBoostKeywords = extractList(content, keywordsLineRe)
		case startsWithCase(p, "Reasoning posture"):
			content := stripLabel(p, "Reasoning posture")
			h.Posture = Posture(firstSentenceKey(content))
		case startsWithCase(p, "Phase 1 asks"):
			h.Phase1Asks = stripLabel(p, "Phase 1 asks")
		case startsWithCase(p, "Anchors"):
			content := stripLabel(p, "Anchors")
			for _, m := range issueRefRe.FindAllStringSubmatch(content, -1) {
				n, err := strconv.Atoi(m[1])
				if err == nil {
					h.AnchorIssueNumbers = append(h.AnchorIssueNumbers, n)
				}
			}
		}
	}
}

func splitParagraphs(body string) []string {
	lines := strings.Split(body, "\n")
	var paras []string
	var current []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(current) > 0 {
				paras = append(paras, strings.Join(current, " "))
				current = current[:0]
			}
			continue
		}
		current = append(current, strings.TrimSpace(l))
	}
	if len(current) > 0 {
		paras = append(paras, strings.Join(current, " "))
	}
	return paras
}

func startsWithCase(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func stripLabel(s, label string) string {
	s = strings.TrimPrefix(s, label)
	s = strings.TrimPrefix(s, strings.ToLower(label))
	s = strings.TrimPrefix(s, ".")
	return strings.TrimSpace(s)
}

func extractList(content string, re *regexp.Regexp) []string {
	m := re.FindStringSubmatch(content)
	if len(m) < 2 {
		return nil
	}
	parts := strings.Split(m[1], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, "`"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstSentenceKey(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "`")
	for i, r := range content {
		if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return strings.ToLower(content[:i])
		}
	}
	return strings.ToLower(content)
}
