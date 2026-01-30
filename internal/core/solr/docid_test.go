package solr

import "testing"

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strip tracking and fragment",
			raw:  "HTTPS://Example.com:443/path/?utm_source=a&b=2&a=1#frag",
			want: "https://example.com/path?a=1&b=2",
		},
		{
			name: "remove trailing slash",
			raw:  "http://example.com/foo/",
			want: "http://example.com/foo",
		},
		{
			name: "remove default port",
			raw:  "http://example.com:80/",
			want: "http://example.com/",
		},
		{
			name: "keep non-default port",
			raw:  "https://example.com:8443/path",
			want: "https://example.com:8443/path",
		},
		{
			name: "strip share param",
			raw:  "https://example.com/path?share=1&c=3",
			want: "https://example.com/path?c=3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanonicalizeURL(tt.raw); got != tt.want {
				t.Fatalf("CanonicalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
