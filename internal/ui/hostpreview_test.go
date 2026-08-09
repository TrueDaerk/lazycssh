package ui

import "testing"

func TestHostPatternPreview(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "empty pattern has nothing to preview",
			pattern: "",
			want:    "",
		},
		{
			name:    "blank pattern has nothing to preview",
			pattern: "   ",
			want:    "",
		},
		{
			name:    "plain hostname previews as itself",
			pattern: "web-01",
			want:    "web-01 (1 host)",
		},
		{
			name:    "small range lists every name",
			pattern: "web{01..03}",
			want:    "web01, web02, web03 (3 hosts)",
		},
		{
			name:    "large range truncates to three names plus the count",
			pattern: "web{01..08}",
			want:    "web01, web02, web03 … (8 hosts)",
		},
		{
			name:    "unmatched brace is a parse error, not a preview",
			pattern: "web{01",
			wantErr: true,
		},
		{
			name:    "bad range is a parse error, not a preview",
			pattern: "web{08..01a}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := hostPatternPreview(tt.pattern)
			if isErr != tt.wantErr {
				t.Fatalf("hostPatternPreview(%q) isErr = %v, want %v (text = %q)", tt.pattern, isErr, tt.wantErr, text)
			}
			if tt.wantErr {
				if text == "" {
					t.Fatalf("hostPatternPreview(%q) returned no error text", tt.pattern)
				}
				return
			}
			if text != tt.want {
				t.Fatalf("hostPatternPreview(%q) = %q, want %q", tt.pattern, text, tt.want)
			}
		})
	}
}
