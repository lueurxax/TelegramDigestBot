package linkextract

import (
	"reflect"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Link
	}{
		{
			name: "single web link",
			text: "Check this out: https://example.com/page",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 16,
				},
			},
		},
		{
			name: "telegram post link with username",
			text: "Telegram post: https://t.me/durov/123",
			want: []Link{
				{
					URL:          "https://t.me/durov/123",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     15,
					TelegramType: "post",
					Username:     "durov",
					MessageID:    123,
				},
			},
		},
		{
			name: "telegram post link with channel ID",
			text: "Private post: https://t.me/c/123456/789",
			want: []Link{
				{
					URL:          "https://t.me/c/123456/789",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     14,
					TelegramType: "post",
					ChannelID:    123456,
					MessageID:    789,
				},
			},
		},
		{
			name: "blocked domain",
			text: "Twitter: https://twitter.com/user/status/123",
			want: []Link{
				{
					URL:      "https://twitter.com/user/status/123",
					Domain:   "twitter.com",
					Type:     LinkTypeBlocked,
					Position: 9,
				},
			},
		},
		{
			name: "multiple links",
			text: "Web: https://google.com and TG: https://t.me/channel/1",
			want: []Link{
				{
					URL:      "https://google.com",
					Domain:   "google.com",
					Type:     LinkTypeWeb,
					Position: 5,
				},
				{
					URL:          "https://t.me/channel/1",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     32,
					TelegramType: "post",
					Username:     "channel",
					MessageID:    1,
				},
			},
		},
		{
			name: "punctuation trimming",
			text: "Link: https://example.com/.",
			want: []Link{
				{
					URL:      "https://example.com/",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 6,
				},
			},
		},
		{
			name: "telegram invite link with plus",
			text: "Join us: https://t.me/+abc123xyz",
			want: []Link{
				{
					URL:          "https://t.me/+abc123xyz",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     9,
					TelegramType: "invite",
				},
			},
		},
		{
			name: "telegram invite link with joinchat",
			text: "Join: https://t.me/joinchat/abc123",
			want: []Link{
				{
					URL:          "https://t.me/joinchat/abc123",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     6,
					TelegramType: "invite",
				},
			},
		},
		{
			name: "telegram channel link without message",
			text: "Channel: https://t.me/telegram",
			want: []Link{
				{
					URL:          "https://t.me/telegram",
					Domain:       "t.me",
					Type:         LinkTypeTelegram,
					Position:     9,
					TelegramType: "channel",
				},
			},
		},
		{
			name: "x.com blocked domain",
			text: "Tweet: https://x.com/user/status/123",
			want: []Link{
				{
					URL:      "https://x.com/user/status/123",
					Domain:   "x.com",
					Type:     LinkTypeBlocked,
					Position: 7,
				},
			},
		},
		{
			name: "instagram blocked domain",
			text: "Post: https://instagram.com/p/abc123",
			want: []Link{
				{
					URL:      "https://instagram.com/p/abc123",
					Domain:   "instagram.com",
					Type:     LinkTypeBlocked,
					Position: 6,
				},
			},
		},
		{
			name: "facebook blocked domain",
			text: "Check: https://facebook.com/post/123",
			want: []Link{
				{
					URL:      "https://facebook.com/post/123",
					Domain:   "facebook.com",
					Type:     LinkTypeBlocked,
					Position: 7,
				},
			},
		},
		{
			name: "linkedin blocked domain",
			text: "Profile: https://linkedin.com/in/user",
			want: []Link{
				{
					URL:      "https://linkedin.com/in/user",
					Domain:   "linkedin.com",
					Type:     LinkTypeBlocked,
					Position: 9,
				},
			},
		},
		{
			name: "tiktok blocked domain",
			text: "Video: https://tiktok.com/@user/video/123",
			want: []Link{
				{
					URL:      "https://tiktok.com/@user/video/123",
					Domain:   "tiktok.com",
					Type:     LinkTypeBlocked,
					Position: 7,
				},
			},
		},
		{
			name: "no links",
			text: "This is just plain text without any links",
			want: nil,
		},
		{
			name: "empty string",
			text: "",
			want: nil,
		},
		{
			name: "duplicate links deduplicated",
			text: "Link: https://example.com and again https://example.com",
			want: []Link{
				{
					URL:      "https://example.com",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 6,
				},
			},
		},
		{
			name: "http link",
			text: "Unsecure: http://example.com/page",
			want: []Link{
				{
					URL:      "http://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 10,
				},
			},
		},
		{
			name: "link with query parameters",
			text: "Search: https://google.com/search?q=test&lang=en",
			want: []Link{
				{
					URL:      "https://google.com/search?lang=en&q=test",
					Domain:   "google.com",
					Type:     LinkTypeWeb,
					Position: 8,
				},
			},
		},
		{
			name: "link with trailing punctuation",
			text: "Check this: https://example.com/page!",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 12,
				},
			},
		},
		{
			name: "link with trailing semicolon",
			text: "Link: https://example.com/page;",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 6,
				},
			},
		},
		{
			name: "link with trailing colon",
			text: "Link: https://example.com/page:",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 6,
				},
			},
		},
		{
			name: "link with trailing question mark",
			text: "Is this a link? https://example.com/page?",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 16,
				},
			},
		},
		{
			name: "link with trailing parenthesis",
			text: "(See https://example.com/page)",
			want: []Link{
				{
					URL:      "https://example.com/page",
					Domain:   "example.com",
					Type:     LinkTypeWeb,
					Position: 5,
				},
			},
		},
		{
			name: "subdomain link",
			text: "API: https://api.example.com/v1",
			want: []Link{
				{
					URL:      "https://api.example.com/v1",
					Domain:   "api.example.com",
					Type:     LinkTypeWeb,
					Position: 5,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractLinks(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "remove tracking params",
			raw:  "https://example.com/path?utm_source=twitter&foo=bar&gclid=123",
			want: "https://example.com/path?foo=bar",
		},
		{
			name: "remove default https port",
			raw:  "https://example.com:443/path/",
			want: "https://example.com/path",
		},
		{
			name: "remove default http port",
			raw:  "http://example.com:80/path?gclid=1&x=2",
			want: "http://example.com/path?x=2",
		},
		{
			name: "strip fragment",
			raw:  "https://example.com/path#section",
			want: "https://example.com/path",
		},
		{
			name: "add scheme for bare domain",
			raw:  "example.com/path?utm_medium=email",
			want: "https://example.com/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeURL(tt.raw); got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractMentions(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single mention",
			text: "Hello @username how are you?",
			want: []string{"username"},
		},
		{
			name: "multiple mentions",
			text: "@user1 and @user2 are here",
			want: []string{"user1", "user2"},
		},
		{
			name: "no mentions",
			text: "Just plain text",
			want: nil,
		},
		{
			name: "empty string",
			text: "",
			want: nil,
		},
		{
			name: "duplicate mentions deduplicated",
			text: "@user hello @user again",
			want: []string{"user"},
		},
		{
			name: "mention with numbers",
			text: "Contact @user123 for help",
			want: []string{"user123"},
		},
		{
			name: "mention with underscore",
			text: "Follow @my_channel please",
			want: []string{"my_channel"},
		},
		{
			name: "too short mention ignored",
			text: "@abc is too short",
			want: nil,
		},
		{
			name: "mention at minimum length",
			text: "@abcd is valid",
			want: []string{"abcd"},
		},
		{
			name: "mention starting with number ignored",
			text: "@123user is invalid",
			want: nil,
		},
		{
			name: "email partially matched as mention",
			text: "Contact email@example.com please",
			want: []string{"example"}, // regex matches @example from email - expected behavior
		},
		{
			name: "mention at start of text",
			text: "@firstuser starts here",
			want: []string{"firstuser"},
		},
		{
			name: "mention at end of text",
			text: "Follow me @lastuser",
			want: []string{"lastuser"},
		},
		{
			name: "long username",
			text: "Contact @verylongusernamehere",
			want: []string{"verylongusernamehere"},
		},
		{
			name: "max length username (31 chars)",
			text: "User @abcdefghijklmnopqrstuvwxyz12345 here",
			want: []string{"abcdefghijklmnopqrstuvwxyz12345"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentions(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractMentions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractURLsFromJSON(t *testing.T) {
	entitiesJSON := []byte(`[{"URL":"http://russiancyprus.news/news/society/test","Length":10,"Offset":0}]`)
	mediaJSON := []byte(`{"Webpage":{"URL":"https://russiancyprus.news/news/society/test2","DisplayURL":"russiancyprus.news/news/society/test2"}}`)

	got := ExtractURLsFromJSON(entitiesJSON, mediaJSON)
	want := []string{
		"http://russiancyprus.news/news/society/test",
		"https://russiancyprus.news/news/society/test2",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractURLsFromJSON() = %v, want %v", got, want)
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "simple domain",
			rawURL: "https://example.com/page",
			want:   "example.com",
		},
		{
			name:   "domain with subdomain",
			rawURL: "https://api.example.com/v1",
			want:   "api.example.com",
		},
		{
			name:   "domain with port",
			rawURL: "https://example.com:8080/page",
			want:   "example.com:8080",
		},
		{
			name:   "uppercase domain normalized",
			rawURL: "https://EXAMPLE.COM/page",
			want:   "example.com",
		},
		{
			name:   "mixed case domain normalized",
			rawURL: "https://Example.COM/page",
			want:   "example.com",
		},
		{
			name:   "invalid URL",
			rawURL: "not a url",
			want:   "",
		},
		{
			name:   "empty URL",
			rawURL: "",
			want:   "",
		},
		{
			name:   "URL with credentials",
			rawURL: "https://user:pass@example.com/page",
			want:   "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.rawURL)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestParseTelegramLink(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		wantTelegramType string
		wantUsername     string
		wantChannelID    int64
		wantMessageID    int64
	}{
		{
			name:             "post with username",
			url:              "https://t.me/durov/123",
			wantTelegramType: "post",
			wantUsername:     "durov",
			wantMessageID:    123,
		},
		{
			name:             "post with channel ID",
			url:              "https://t.me/c/123456/789",
			wantTelegramType: "post",
			wantChannelID:    123456,
			wantMessageID:    789,
		},
		{
			name:             "invite link with plus",
			url:              "https://t.me/+abc123",
			wantTelegramType: "invite",
		},
		{
			name:             "invite link with joinchat",
			url:              "https://t.me/joinchat/abc123",
			wantTelegramType: "invite",
		},
		{
			name:             "channel link",
			url:              "https://t.me/telegram",
			wantTelegramType: "channel",
		},
		{
			name:             "channel link with short username",
			url:              "https://t.me/news",
			wantTelegramType: "channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &Link{URL: tt.url}
			parseTelegramLink(link)

			if link.TelegramType != tt.wantTelegramType {
				t.Errorf("TelegramType = %q, want %q", link.TelegramType, tt.wantTelegramType)
			}

			if link.Username != tt.wantUsername {
				t.Errorf("Username = %q, want %q", link.Username, tt.wantUsername)
			}

			if link.ChannelID != tt.wantChannelID {
				t.Errorf("ChannelID = %d, want %d", link.ChannelID, tt.wantChannelID)
			}

			if link.MessageID != tt.wantMessageID {
				t.Errorf("MessageID = %d, want %d", link.MessageID, tt.wantMessageID)
			}
		})
	}
}
