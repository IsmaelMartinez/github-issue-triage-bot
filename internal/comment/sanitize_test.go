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
