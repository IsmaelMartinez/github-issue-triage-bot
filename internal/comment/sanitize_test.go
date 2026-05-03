package comment

import "testing"

func TestSanitizeLLMOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "javascript link",
			in:   "[click](javascript:alert(1))",
			want: "[click](removed)",
		},
		{
			name: "data link",
			in:   "[x](data:text/html,<script>)",
			want: "[x](removed)",
		},
		{
			name: "safe https link",
			in:   "[docs](https://example.com)",
			want: "[docs](https://example.com)",
		},
		{
			name: "raw html tag",
			in:   "text <script>alert(1)</script> more",
			want: "text  more",
		},
		{
			name: "markdown preserved",
			in:   "**bold** and `code`",
			want: "**bold** and `code`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLLMOutput(tt.in)
			if got != tt.want {
				t.Fatalf("sanitizeLLMOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeLLMOutputStripsGFMImages(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tracking pixel",
			input: "Try this fix: ![](https://tracker.example.com/pixel)",
			want:  "Try this fix: ",
		},
		{
			name:  "image with alt text",
			input: "See ![screenshot](https://evil.com/img.png) for details",
			want:  "See  for details",
		},
		{
			name:  "regular markdown link preserved",
			input: "See [this guide](https://github.com/docs) for details",
			want:  "See [this guide](https://github.com/docs) for details",
		},
		{
			name:  "reference-style image stripped",
			input: "See ![alt][img]\n\n[img]: https://tracker.example.com/pixel.png",
			want:  "See \n",
		},
		{
			name:  "reference-style link definition stripped",
			input: "text\n[ref]: https://evil.com/pixel\nmore text",
			want:  "text\n\nmore text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLLMOutput(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLLMOutput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractUpstreamRefs(t *testing.T) {
	tests := []struct {
		name string
		urls []string
		want []UpstreamRef
	}{
		{
			name: "issue URL parsed",
			urls: []string{"https://github.com/electron/electron/issues/50106"},
			want: []UpstreamRef{{Owner: "electron", Repo: "electron", Number: 50106}},
		},
		{
			name: "pull URL parsed",
			urls: []string{"https://github.com/electron/electron/pull/50300"},
			want: []UpstreamRef{{Owner: "electron", Repo: "electron", Number: 50300}},
		},
		{
			name: "non-issue URL ignored",
			urls: []string{"https://example.com/docs/login", "https://github.com/electron/electron/releases/tag/v39.0.0"},
			want: nil,
		},
		{
			name: "mixed URLs return only issue/PR",
			urls: []string{
				"https://example.com/docs",
				"https://github.com/electron/electron/issues/50106",
				"",
			},
			want: []UpstreamRef{{Owner: "electron", Repo: "electron", Number: 50106}},
		},
		{
			name: "empty input",
			urls: nil,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUpstreamRefs(tt.urls)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractUpstreamRefs() len = %d, want %d (got=%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractUpstreamRefs()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRewriteBareUpstreamRefs(t *testing.T) {
	electron := []UpstreamRef{{Owner: "electron", Repo: "electron", Number: 50106}}

	tests := []struct {
		name       string
		in         string
		refs       []UpstreamRef
		localHints map[int]bool
		want       string
	}{
		{
			name: "rewrites bare upstream ref",
			in:   "this looks like #50106 in Electron",
			refs: electron,
			want: "this looks like electron/electron#50106 in Electron",
		},
		{
			name: "leaves unrelated bare ref unchanged (local default)",
			in:   "see #123 for context",
			refs: electron,
			want: "see #123 for context",
		},
		{
			name:       "ambiguity prefers local when hint set matches",
			in:         "see #50106 in this repo",
			refs:       electron,
			localHints: map[int]bool{50106: true},
			want:       "see #50106 in this repo",
		},
		{
			name: "idempotent on already-qualified output",
			in:   "this looks like electron/electron#50106 in Electron",
			refs: electron,
			want: "this looks like electron/electron#50106 in Electron",
		},
		{
			name: "leaves already-qualified ref unchanged",
			in:   "duplicate of electron/electron#50106",
			refs: electron,
			want: "duplicate of electron/electron#50106",
		},
		{
			name: "leaves number inside markdown link unchanged",
			in:   "see [#50106](https://github.com/electron/electron/issues/50106) for details",
			refs: electron,
			want: "see [#50106](https://github.com/electron/electron/issues/50106) for details",
		},
		{
			name: "leaves number inside bare URL unchanged",
			in:   "see https://github.com/electron/electron/issues/50106 for details",
			refs: electron,
			want: "see https://github.com/electron/electron/issues/50106 for details",
		},
		{
			name: "no upstream refs returns text untouched",
			in:   "see #50106 for details",
			refs: nil,
			want: "see #50106 for details",
		},
		{
			name: "rewrites at start of string",
			in:   "#50106 is the upstream report",
			refs: electron,
			want: "electron/electron#50106 is the upstream report",
		},
		{
			name: "punctuation after bare ref preserved",
			in:   "fixed by #50106. Try the latest build.",
			refs: electron,
			want: "fixed by electron/electron#50106. Try the latest build.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteBareUpstreamRefs(tt.in, tt.refs, tt.localHints)
			if got != tt.want {
				t.Fatalf("rewriteBareUpstreamRefs() =\n  %q\nwant\n  %q", got, tt.want)
			}
			// Idempotency: applying twice produces the same output.
			twice := rewriteBareUpstreamRefs(got, tt.refs, tt.localHints)
			if twice != got {
				t.Fatalf("rewriteBareUpstreamRefs() not idempotent:\n  first  = %q\n  second = %q", got, twice)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"https URL", "https://example.com", "https://example.com"},
		{"http URL", "http://example.com", "http://example.com"},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"JavaScript mixed case", "JavaScript:void(0)", ""},
		{"data scheme", "data:text/html,<script>alert(1)</script>", ""},
		{"vbscript scheme", "vbscript:MsgBox", ""},
		{"empty string", "", ""},
		{"whitespace padded", "  https://example.com  ", "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeURL(tt.in)
			if got != tt.want {
				t.Fatalf("sanitizeURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
