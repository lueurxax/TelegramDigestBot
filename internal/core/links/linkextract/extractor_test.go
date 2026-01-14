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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractLinks(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}
