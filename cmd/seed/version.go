package main

import "regexp"

var electronVersionRe = regexp.MustCompile(`v?(\d+)\.\d+`)

func extractElectronVersion(topic string) string {
	m := electronVersionRe.FindStringSubmatch(topic)
	if m == nil {
		return ""
	}
	return m[1]
}
