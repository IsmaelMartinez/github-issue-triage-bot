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
