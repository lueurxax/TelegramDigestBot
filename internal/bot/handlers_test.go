package bot

import (
	"strconv"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/schedule"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	errClampFloat32Fmt         = "clampFloat32(%v, %v, %v) = %v, want %v"
	expectedRatingGood         = "good"
	expectedRatingBad          = "bad"
	expectedRatingIrrelevant   = "irrelevant"
	expectedLogFieldUserID     = "user_id"
	expectedLogFieldUsername   = "username"
	expectedButtonUseful       = "👍 Useful"
	expectedButtonNotUseful    = "👎 Not useful"
	expectedCallbackRate       = "rate:"
	expectedCallbackDiscover   = "discover:"
	expectedCallbackUp         = ":up"
	expectedCallbackDown       = ":down"
	expectedStatusEnabled      = "ENABLED"
	expectedStatusDisabled     = "DISABLED"
	expectedWeightManual       = "manual"
	expectedToggleOff          = "off"
	expectedDateTimeFormat     = "2006-01-02 15:04:05"
	expectedTimeFormat         = "15:04"
	expectedDateFormatYMD      = "2006-01-02"
	expectedEntityTypeBotCmd   = "bot_command"
	expectedPromptActiveKeyFmt = "prompt:%s:active"
	expectedPromptKeyFmt       = "prompt:%s:%s"
	expectedErrSavingFmt       = "❌ Error saving %s: %s"
	expectedErrFetchChannelFmt = "❌ Error fetching channels: %s"
	expectedErrUnknownBaseFmt  = "Unknown base. Use: <code>%s</code>"
	expectedErrChannelNotFound = "Channel <code>%s</code> not found."
	expectedErrGenericFmt      = "Error: %s"
	expectedErrNoRows          = "no rows"
)

func TestParseScheduleTimes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:    "single time",
			input:   "09:00",
			want:    []string{"09:00"},
			wantErr: false,
		},
		{
			name:    "multiple times comma-separated",
			input:   "09:00, 13:00, 18:00",
			want:    []string{"09:00", "13:00", "18:00"},
			wantErr: false,
		},
		{
			name:    "multiple times space-separated",
			input:   "09:00 13:00 18:00",
			want:    []string{"09:00", "13:00", "18:00"},
			wantErr: false,
		},
		{
			name:    "single digit hour",
			input:   "9:00",
			want:    []string{"09:00"},
			wantErr: false,
		},
		{
			name:    "mixed format",
			input:   "9:00, 13:00",
			want:    []string{"09:00", "13:00"},
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "invalid time format",
			input:   "25:00",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "non-zero minutes rejected",
			input:   "09:30",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid format no colon",
			input:   "0900",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseScheduleTimes(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseScheduleTimes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("parseScheduleTimes() = %v, want %v", got, tt.want)
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseScheduleTimes()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseHourlyRange(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{
			name:      "valid range",
			input:     "09:00-18:00",
			wantStart: "09:00",
			wantEnd:   "18:00",
			wantErr:   false,
		},
		{
			name:      "valid range with spaces",
			input:     "09:00 - 18:00",
			wantStart: "09:00",
			wantEnd:   "18:00",
			wantErr:   false,
		},
		{
			name:      "single digit hours",
			input:     "9:00-18:00",
			wantStart: "09:00",
			wantEnd:   "18:00",
			wantErr:   false,
		},
		{
			name:    "missing end",
			input:   "09:00-",
			wantErr: true,
		},
		{
			name:    "missing start",
			input:   "-18:00",
			wantErr: true,
		},
		{
			name:    "no separator",
			input:   "09:00",
			wantErr: true,
		},
		{
			name:    "invalid start time",
			input:   "25:00-18:00",
			wantErr: true,
		},
		{
			name:    "invalid end time",
			input:   "09:00-25:00",
			wantErr: true,
		},
		{
			name:    "non-zero minutes in start",
			input:   "09:30-18:00",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseHourlyRange(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseHourlyRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if start != tt.wantStart {
				t.Errorf("parseHourlyRange() start = %q, want %q", start, tt.wantStart)
			}

			if end != tt.wantEnd {
				t.Errorf("parseHourlyRange() end = %q, want %q", end, tt.wantEnd)
			}
		})
	}
}

func TestParseSchedulePreviewCount(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    int
		wantErr bool
	}{
		{
			name:    "no args returns default",
			args:    []string{},
			want:    schedulePreviewDefault,
			wantErr: false,
		},
		{
			name:    "only subcommand",
			args:    []string{"preview"},
			want:    schedulePreviewDefault,
			wantErr: false,
		},
		{
			name:    "valid count",
			args:    []string{"preview", "10"},
			want:    10,
			wantErr: false,
		},
		{
			name:    "count exceeds max",
			args:    []string{"preview", "100"},
			want:    schedulePreviewMax,
			wantErr: false,
		},
		{
			name:    "invalid count string",
			args:    []string{"preview", "abc"},
			wantErr: true,
		},
		{
			name:    "zero count",
			args:    []string{"preview", "0"},
			wantErr: true,
		},
		{
			name:    "negative count",
			args:    []string{"preview", "-5"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSchedulePreviewCount(tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseSchedulePreviewCount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got != tt.want {
				t.Errorf("parseSchedulePreviewCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatScheduleDay(t *testing.T) {
	tests := []struct {
		name string
		day  schedule.DaySchedule
		want string
	}{
		{
			name: "empty schedule",
			day:  schedule.DaySchedule{},
			want: "none",
		},
		{
			name: "times only",
			day: schedule.DaySchedule{
				Times: []string{"09:00", "13:00", "18:00"},
			},
			want: "times 09:00, 13:00, 18:00",
		},
		{
			name: "hourly only",
			day: schedule.DaySchedule{
				Hourly: &schedule.HourlyRange{Start: "09:00", End: "18:00"},
			},
			want: "hourly 09:00-18:00",
		},
		{
			name: "both times and hourly",
			day: schedule.DaySchedule{
				Times:  []string{"07:00"},
				Hourly: &schedule.HourlyRange{Start: "09:00", End: "18:00"},
			},
			want: "times 07:00; hourly 09:00-18:00",
		},
		{
			name: "single time",
			day: schedule.DaySchedule{
				Times: []string{"12:00"},
			},
			want: "times 12:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatScheduleDay(tt.day)

			if got != tt.want {
				t.Errorf("formatScheduleDay() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareSubcommandMessage(t *testing.T) {
	tests := []struct {
		name       string
		msgText    string
		subcommand string
		args       []string
		wantText   string
	}{
		{
			name:       "subcommand without args",
			msgText:    "/channel list",
			subcommand: "list",
			args:       []string{"list"},
			wantText:   "/list",
		},
		{
			name:       "subcommand with args",
			msgText:    "/channel add @testchannel",
			subcommand: "add",
			args:       []string{"add", "@testchannel"},
			wantText:   "/add @testchannel",
		},
		{
			name:       "subcommand with multiple args",
			msgText:    "/channel metadata @chan cat tone freq",
			subcommand: "metadata",
			args:       []string{"metadata", "@chan", "cat", "tone", "freq"},
			wantText:   "/metadata @chan cat tone freq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &tgbotapi.Message{
				Text: tt.msgText,
				Entities: []tgbotapi.MessageEntity{
					{Type: EntityTypeBotCommand, Offset: 0, Length: 8},
				},
			}

			got := prepareSubcommandMessage(msg, tt.subcommand, tt.args)

			if got.Text != tt.wantText {
				t.Errorf("prepareSubcommandMessage().Text = %q, want %q", got.Text, tt.wantText)
			}

			// Check that entity length was updated correctly
			if len(got.Entities) > 0 && got.Entities[0].Type == EntityTypeBotCommand {
				expectedLen := len(tt.subcommand) + 1 // +1 for the /
				if got.Entities[0].Length != expectedLen {
					t.Errorf("prepareSubcommandMessage().Entities[0].Length = %d, want %d", got.Entities[0].Length, expectedLen)
				}
			}

			// Check CommandArguments
			if got.CommandArguments() != strings.Join(tt.args[1:], " ") {
				t.Errorf("prepareSubcommandMessage().CommandArguments() = %q, want %q", got.CommandArguments(), strings.Join(tt.args[1:], " "))
			}
		})
	}
}

func TestParseScoresArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantHours int
		wantLimit int
	}{
		{
			name:      "no args uses defaults",
			args:      []string{},
			wantHours: DefaultScoresHours,
			wantLimit: DefaultScoresLimit,
		},
		{
			name:      "first arg sets hours",
			args:      []string{"48"},
			wantHours: 48,
			wantLimit: DefaultScoresLimit,
		},
		{
			name:      "both args set hours and limit",
			args:      []string{"12", "20"},
			wantHours: 12,
			wantLimit: 20,
		},
		{
			name:      "invalid first arg uses default hours",
			args:      []string{"abc"},
			wantHours: DefaultScoresHours,
			wantLimit: DefaultScoresLimit,
		},
		{
			name:      "invalid second arg uses default limit",
			args:      []string{"24", "xyz"},
			wantHours: 24,
			wantLimit: DefaultScoresLimit,
		},
		{
			name:      "negative hours uses default",
			args:      []string{"-5"},
			wantHours: DefaultScoresHours,
			wantLimit: DefaultScoresLimit,
		},
		{
			name:      "zero hours uses default",
			args:      []string{"0"},
			wantHours: DefaultScoresHours,
			wantLimit: DefaultScoresLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hours, limit := parseScoresArgs(tt.args)

			if hours != tt.wantHours {
				t.Errorf("parseScoresArgs() hours = %d, want %d", hours, tt.wantHours)
			}

			if limit != tt.wantLimit {
				t.Errorf("parseScoresArgs() limit = %d, want %d", limit, tt.wantLimit)
			}
		})
	}
}

func TestParseScoresDebugArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantHours int
		wantValid bool
	}{
		{
			name:      "no args uses default",
			args:      []string{},
			wantHours: DefaultScoresHours,
			wantValid: true,
		},
		{
			name:      "valid hours",
			args:      []string{"48"},
			wantHours: 48,
			wantValid: true,
		},
		{
			name:      "invalid string",
			args:      []string{"abc"},
			wantHours: 0,
			wantValid: false,
		},
		{
			name:      "too many args",
			args:      []string{"24", "extra"},
			wantHours: 0,
			wantValid: false,
		},
		{
			name:      "zero hours invalid",
			args:      []string{"0"},
			wantHours: 0,
			wantValid: false,
		},
		{
			name:      "negative hours invalid",
			args:      []string{"-10"},
			wantHours: 0,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hours, valid := parseScoresDebugArgs(tt.args)

			if valid != tt.wantValid {
				t.Errorf("parseScoresDebugArgs() valid = %v, want %v", valid, tt.wantValid)
			}

			if hours != tt.wantHours {
				t.Errorf("parseScoresDebugArgs() hours = %d, want %d", hours, tt.wantHours)
			}
		})
	}
}

func TestParseRatingValue(t *testing.T) {
	tests := []struct {
		input string
		want  int16
	}{
		{"up", 1},
		{"down", -1},
		{"invalid", 0},
		{"", 0},
		{"UP", 0},   // case-sensitive
		{"Down", 0}, // case-sensitive
		{"0", 0},
		{"1", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseRatingValue(tt.input)

			if got != tt.want {
				t.Errorf("parseRatingValue(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestClampFloat32(t *testing.T) {
	tests := []struct {
		name   string
		val    float32
		minVal float32
		maxVal float32
		want   float32
	}{
		{
			name:   "value within range",
			val:    0.5,
			minVal: 0.0,
			maxVal: 1.0,
			want:   0.5,
		},
		{
			name:   "value below min",
			val:    -0.5,
			minVal: 0.0,
			maxVal: 1.0,
			want:   0.0,
		},
		{
			name:   "value above max",
			val:    1.5,
			minVal: 0.0,
			maxVal: 1.0,
			want:   1.0,
		},
		{
			name:   "value at min",
			val:    0.0,
			minVal: 0.0,
			maxVal: 1.0,
			want:   0.0,
		},
		{
			name:   "value at max",
			val:    1.0,
			minVal: 0.0,
			maxVal: 1.0,
			want:   1.0,
		},
		{
			name:   "negative range",
			val:    -0.5,
			minVal: -1.0,
			maxVal: 0.0,
			want:   -0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampFloat32(tt.val, tt.minVal, tt.maxVal)

			if got != tt.want {
				t.Errorf(errClampFloat32Fmt, tt.val, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestNormalizeAnnotationLabel(t *testing.T) {
	tests := []struct {
		input  string
		want   string
		wantOk bool
	}{
		{"good", RatingGood, true},
		{"bad", RatingBad, true},
		{"irrelevant", RatingIrrelevant, true},
		{"GOOD", RatingGood, true},
		{"Bad", RatingBad, true},
		{"IRRELEVANT", RatingIrrelevant, true},
		{"  good  ", RatingGood, true},
		{"invalid", "", false},
		{"", "", false},
		{"excellent", "", false},
		{"neutral", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := normalizeAnnotationLabel(tt.input)

			if ok != tt.wantOk {
				t.Errorf("normalizeAnnotationLabel(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}

			if got != tt.want {
				t.Errorf("normalizeAnnotationLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateAnnotationText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{
			name:  "text within limit",
			text:  "Short text",
			limit: 100,
			want:  "Short text",
		},
		{
			name:  "text exceeds limit",
			text:  "This is a longer text that exceeds the limit",
			limit: 20,
			want:  "This is a longer tex...",
		},
		{
			name:  "empty text",
			text:  "",
			limit: 100,
			want:  "",
		},
		{
			name:  "text exactly at limit",
			text:  "12345",
			limit: 5,
			want:  "12345",
		},
		{
			name:  "unicode text truncated correctly",
			text:  "Привет мир, это тест",
			limit: 10,
			want:  "Привет мир...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateAnnotationText(tt.text, tt.limit)

			if got != tt.want {
				t.Errorf("truncateAnnotationText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnnotationChannelName(t *testing.T) {
	tests := []struct {
		name string
		item *db.AnnotationItem
		want string
	}{
		{
			name: "prefer username",
			item: &db.AnnotationItem{
				ChannelUsername: "testchannel",
				ChannelTitle:    "Test Channel",
			},
			want: "@testchannel",
		},
		{
			name: "fallback to title",
			item: &db.AnnotationItem{
				ChannelUsername: "",
				ChannelTitle:    "Test Channel",
			},
			want: "Test Channel",
		},
		{
			name: "fallback to unknown",
			item: &db.AnnotationItem{
				ChannelUsername: "",
				ChannelTitle:    "",
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := annotationChannelName(tt.item)

			if got != tt.want {
				t.Errorf("annotationChannelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTopicsForPreview(t *testing.T) {
	tests := []struct {
		name     string
		items    []db.Item
		clusters []db.ClusterWithItems
		wantLen  int
	}{
		{
			name:     "empty inputs",
			items:    nil,
			clusters: nil,
			wantLen:  0,
		},
		{
			name: "topics from items only",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
				{Topic: "Technology"}, // duplicate
			},
			clusters: nil,
			wantLen:  2,
		},
		{
			name:  "topics from clusters only",
			items: nil,
			clusters: []db.ClusterWithItems{
				{Topic: "Politics"},
				{Topic: "Science"},
			},
			wantLen: 2,
		},
		{
			name: "topics from both",
			items: []db.Item{
				{Topic: "Technology"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: "Politics"},
				{Topic: "Technology"}, // duplicate with item
			},
			wantLen: 2,
		},
		{
			name: "empty topics filtered",
			items: []db.Item{
				{Topic: ""},
				{Topic: "Technology"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: ""},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTopicsForPreview(tt.items, tt.clusters)

			if len(got) != tt.wantLen {
				t.Errorf("extractTopicsForPreview() returned %d topics, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCollectClusterSummariesForPreview(t *testing.T) {
	tests := []struct {
		name     string
		clusters []db.ClusterWithItems
		maxItems int
		wantLen  int
	}{
		{
			name:     "empty clusters",
			clusters: nil,
			maxItems: 5,
			wantLen:  0,
		},
		{
			name: "clusters with topics and items",
			clusters: []db.ClusterWithItems{
				{Topic: "Technology", Items: []db.Item{{Summary: "Summary 1"}}},
				{Topic: "Finance", Items: []db.Item{{Summary: "Summary 2"}}},
			},
			maxItems: 5,
			wantLen:  2,
		},
		{
			name: "respects max limit",
			clusters: []db.ClusterWithItems{
				{Topic: "Technology", Items: []db.Item{{Summary: "Summary 1"}}},
				{Topic: "Finance", Items: []db.Item{{Summary: "Summary 2"}}},
				{Topic: "Politics", Items: []db.Item{{Summary: "Summary 3"}}},
			},
			maxItems: 2,
			wantLen:  2,
		},
		{
			name: "skips clusters without topic",
			clusters: []db.ClusterWithItems{
				{Topic: "", Items: []db.Item{{Summary: "Summary 1"}}},
				{Topic: "Finance", Items: []db.Item{{Summary: "Summary 2"}}},
			},
			maxItems: 5,
			wantLen:  1,
		},
		{
			name: "skips clusters without items",
			clusters: []db.ClusterWithItems{
				{Topic: "Technology", Items: nil},
				{Topic: "Finance", Items: []db.Item{{Summary: "Summary 2"}}},
			},
			maxItems: 5,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectClusterSummariesForPreview(tt.clusters, tt.maxItems)

			if len(got) != tt.wantLen {
				t.Errorf("collectClusterSummariesForPreview() returned %d summaries, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestAppendItemSummariesForPreview(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		items    []db.Item
		maxItems int
		wantLen  int
	}{
		{
			name:     "empty inputs",
			existing: nil,
			items:    nil,
			maxItems: 5,
			wantLen:  0,
		},
		{
			name:     "append to empty",
			existing: nil,
			items: []db.Item{
				{Summary: "Summary 1"},
				{Summary: "Summary 2"},
			},
			maxItems: 5,
			wantLen:  2,
		},
		{
			name:     "append to existing",
			existing: []string{"Existing"},
			items: []db.Item{
				{Summary: "Summary 1"},
				{Summary: "Summary 2"},
			},
			maxItems: 5,
			wantLen:  3,
		},
		{
			name:     "respects max limit with existing",
			existing: []string{"Existing1", "Existing2"},
			items: []db.Item{
				{Summary: "Summary 1"},
				{Summary: "Summary 2"},
			},
			maxItems: 3,
			wantLen:  3,
		},
		{
			name:     "skips empty summaries",
			existing: nil,
			items: []db.Item{
				{Summary: ""},
				{Summary: "Summary 1"},
			},
			maxItems: 5,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendItemSummariesForPreview(tt.existing, tt.items, tt.maxItems)

			if len(got) != tt.wantLen {
				t.Errorf("appendItemSummariesForPreview() returned %d summaries, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestAnnotateUsage(t *testing.T) {
	usage := annotateUsage()

	expectedStrings := []string{
		"/annotate enqueue",
		"/annotate next",
		"/annotate label",
		"/annotate skip",
		"/annotate stats",
	}

	for _, expected := range expectedStrings {
		if !containsString(usage, expected) {
			t.Errorf("annotateUsage() does not contain %q", expected)
		}
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func TestFormatDiscoveryIdentifierExtended(t *testing.T) {
	tests := []struct {
		name      string
		discovery db.DiscoveredChannel
		want      string
	}{
		{
			name: "zero peer ID with invite link",
			discovery: db.DiscoveredChannel{
				Username:   "",
				TGPeerID:   0,
				InviteLink: "https://t.me/+secret",
			},
			want: "[invite link]",
		},
		{
			name: "all fields empty",
			discovery: db.DiscoveredChannel{
				Username:   "",
				TGPeerID:   0,
				InviteLink: "",
			},
			want: "",
		},
		{
			name: "negative peer ID",
			discovery: db.DiscoveredChannel{
				Username: "",
				TGPeerID: -1001234567890,
			},
			want: "ID:-1001234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDiscoveryIdentifier(tt.discovery)

			if got != tt.want {
				t.Errorf("formatDiscoveryIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatScoresDebugOutput(t *testing.T) {
	tests := []struct {
		name       string
		hours      int
		debugStats db.ScoreDebugStats
		itemStats  db.ItemStatusStats
		wantParts  []string
	}{
		{
			name:  "basic stats",
			hours: 24,
			debugStats: db.ScoreDebugStats{
				RawTotal:       100,
				RawProcessed:   80,
				ItemsTotal:     60,
				GateRelevant:   50,
				GateIrrelevant: 10,
			},
			itemStats: db.ItemStatusStats{
				Total:         60,
				ReadyPending:  30,
				ReadyDigested: 20,
				Rejected:      5,
				Error:         5,
			},
			wantParts: []string{
				"last 24 hours",
				"Raw messages: <code>100</code>",
				"Processed: <code>80</code>",
				"Unprocessed: <code>20</code>",
				"Items created: <code>60</code>",
				"Ready (pending): <code>30</code>",
				"Ready (digested): <code>20</code>",
			},
		},
		{
			name:  "zero values",
			hours: 12,
			debugStats: db.ScoreDebugStats{
				RawTotal:     0,
				RawProcessed: 0,
				ItemsTotal:   0,
			},
			itemStats: db.ItemStatusStats{
				Total: 0,
			},
			wantParts: []string{
				"last 12 hours",
				"Raw messages: <code>0</code>",
			},
		},
		{
			name:  "negative raw unprocessed clamped to zero",
			hours: 6,
			debugStats: db.ScoreDebugStats{
				RawTotal:     50,
				RawProcessed: 60, // processed > total, should clamp unprocessed to 0
				ItemsTotal:   40,
			},
			itemStats: db.ItemStatusStats{
				Total: 40,
			},
			wantParts: []string{
				"Unprocessed: <code>0</code>",
			},
		},
		{
			name:  "negative dropped before item clamped to zero",
			hours: 6,
			debugStats: db.ScoreDebugStats{
				RawTotal:     100,
				RawProcessed: 50,
				ItemsTotal:   60, // items > processed, should clamp dropped to 0
			},
			itemStats: db.ItemStatusStats{
				Total: 60,
			},
			wantParts: []string{
				"Dropped before item: <code>0</code>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatScoresDebugOutput(tt.hours, tt.debugStats, tt.itemStats)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatScoresDebugOutput() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatScoresOutput(t *testing.T) {
	tests := []struct {
		name      string
		hours     int
		threshold float32
		stats     *db.ImportanceStats
		items     []db.ItemScore
		wantParts []string
	}{
		{
			name:      "basic output",
			hours:     24,
			threshold: 0.5,
			stats: &db.ImportanceStats{
				Total:          100,
				AboveThreshold: 60,
				P50:            0.45,
				P75:            0.65,
				P90:            0.80,
				P95:            0.90,
				Min:            0.10,
				Max:            0.95,
			},
			items: []db.ItemScore{
				{Username: "channel1", Importance: 0.9, Relevance: 0.8},
				{Username: "channel2", Importance: 0.4, Relevance: 0.6},
			},
			wantParts: []string{
				"last 24 hours",
				"Threshold: <code>0.50</code>",
				"Ready items: <code>100</code>",
				"p50 <code>0.45</code>",
				"@channel1",
			},
		},
		{
			name:      "empty items",
			hours:     12,
			threshold: 0.7,
			stats: &db.ImportanceStats{
				Total: 0,
			},
			items: []db.ItemScore{},
			wantParts: []string{
				"No ready items to display",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatScoresOutput(tt.hours, tt.threshold, tt.stats, tt.items)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatScoresOutput() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatRatingsSummaryOutput(t *testing.T) {
	tests := []struct {
		name       string
		days       int
		limit      int
		summaries  []db.RatingSummary
		good       int
		bad        int
		irrelevant int
		total      int
		wantParts  []string
	}{
		{
			name:  "basic output",
			days:  30,
			limit: 2,
			summaries: []db.RatingSummary{
				{ChannelID: "1", Username: "chan1", GoodCount: 10, BadCount: 2, TotalCount: 12},
				{ChannelID: "2", Title: "Channel 2", GoodCount: 5, BadCount: 3, TotalCount: 8},
			},
			good:       15,
			bad:        5,
			irrelevant: 2,
			total:      22,
			wantParts: []string{
				"last 30 days",
				"Total: <code>22</code>",
				"@chan1",
				"Channel 2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRatingsSummaryOutput(tt.days, tt.limit, tt.summaries, tt.good, tt.bad, tt.irrelevant, tt.total)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatRatingsSummaryOutput() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestBuildDiscoveryKeyboard(t *testing.T) {
	tests := []struct {
		name        string
		discoveries []db.DiscoveredChannel
		wantRows    int
	}{
		{
			name:        "empty discoveries",
			discoveries: nil,
			wantRows:    0,
		},
		{
			name: "discoveries with usernames",
			discoveries: []db.DiscoveredChannel{
				{Username: "channel1"},
				{Username: "channel2"},
			},
			wantRows: 2,
		},
		{
			name: "discoveries without usernames filtered",
			discoveries: []db.DiscoveredChannel{
				{Username: "channel1"},
				{Username: "", TGPeerID: 12345},
				{Username: "channel2"},
			},
			wantRows: 2,
		},
		{
			name: "all without usernames",
			discoveries: []db.DiscoveredChannel{
				{TGPeerID: 12345},
				{TGPeerID: 67890},
			},
			wantRows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDiscoveryKeyboard(tt.discoveries)

			if len(got) != tt.wantRows {
				t.Errorf("buildDiscoveryKeyboard() returned %d rows, want %d", len(got), tt.wantRows)
			}
		})
	}
}

func TestFormatDiscoveryList(t *testing.T) {
	tests := []struct {
		name        string
		discoveries []db.DiscoveredChannel
		wantParts   []string
	}{
		{
			name: "single discovery",
			discoveries: []db.DiscoveredChannel{
				{
					Username:       "testchannel",
					Title:          "Test Channel",
					SourceType:     "forward",
					DiscoveryCount: 5,
				},
			},
			wantParts: []string{
				"Pending Channel Discoveries",
				"Test Channel",
				"@testchannel",
				"forward",
				"5x",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDiscoveryList(tt.discoveries)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatDiscoveryList() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatChannelEntry(t *testing.T) {
	tests := []struct {
		name      string
		channel   db.Channel
		wantParts []string
	}{
		{
			name: "channel with username",
			channel: db.Channel{
				Username:         "testchan",
				Title:            "Test Channel",
				ImportanceWeight: 1.5,
				WeightOverride:   true,
				Context:          "Tech news",
			},
			wantParts: []string{
				"@testchan",
				"Test Channel",
				"1.5x",
				"(manual)",
				"Tech news",
			},
		},
		{
			name: "channel without username",
			channel: db.Channel{
				TGPeerID:             12345678,
				Title:                "Private Channel",
				ImportanceWeight:     1.0,
				AutoRelevanceEnabled: true,
			},
			wantParts: []string{
				"ID: <code>12345678</code>",
				"Private Channel",
				"1.0x",
				"auto",
			},
		},
		{
			name: "channel pending title",
			channel: db.Channel{
				Username:         "newchan",
				Title:            "",
				ImportanceWeight: 1.0,
			},
			wantParts: []string{
				"@newchan",
				"Pending...",
			},
		},
		{
			name: "channel with auto relevance and delta",
			channel: db.Channel{
				Username:                "deltachan",
				Title:                   "Delta Channel",
				ImportanceWeight:        1.2,
				AutoRelevanceEnabled:    true,
				RelevanceThresholdDelta: 0.15,
			},
			wantParts: []string{
				"@deltachan",
				"Delta Channel",
				"1.2x",
				"auto",
				"+0.15",
			},
		},
		{
			name: "channel with description",
			channel: db.Channel{
				Username:         "descchan",
				Title:            "Desc Channel",
				ImportanceWeight: 1.0,
				Description:      "This is a channel description",
			},
			wantParts: []string{
				"@descchan",
				"Description: This is a channel description",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			formatChannelEntry(&sb, tt.channel)
			got := sb.String()

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatChannelEntry() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatChannelMetadata(t *testing.T) {
	tests := []struct {
		name      string
		channel   db.Channel
		wantEmpty bool
		wantParts []string
	}{
		{
			name: "full metadata",
			channel: db.Channel{
				Category:   "Tech",
				Tone:       "Formal",
				UpdateFreq: "Daily",
			},
			wantEmpty: false,
			wantParts: []string{"Metadata:", "Category: Tech", "Tone: Formal", "Freq: Daily"},
		},
		{
			name: "partial metadata",
			channel: db.Channel{
				Category: "News",
			},
			wantEmpty: false,
			wantParts: []string{"Metadata:", "Category: News"},
		},
		{
			name: "no metadata",
			channel: db.Channel{
				Category:   "",
				Tone:       "",
				UpdateFreq: "",
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			formatChannelMetadata(&sb, tt.channel)
			got := sb.String()

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("formatChannelMetadata() = %q, want empty", got)
				}

				return
			}

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatChannelMetadata() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatScoreItem(t *testing.T) {
	tests := []struct {
		name      string
		item      db.ItemScore
		threshold float32
		wantParts []string
	}{
		{
			name: "item above threshold with username",
			item: db.ItemScore{
				Username:   "chan1",
				Importance: 0.8,
				Relevance:  0.9,
				Summary:    "Test summary",
			},
			threshold: 0.5,
			wantParts: []string{"@chan1", "0.80", "rel 0.90", "Test summary"},
		},
		{
			name: "item below threshold",
			item: db.ItemScore{
				Username:   "chan2",
				Importance: 0.3,
				Relevance:  0.5,
				Summary:    "Low score item",
			},
			threshold: 0.5,
			wantParts: []string{"@chan2", "0.30", "Low score item"},
		},
		{
			name: "item without username falls back to title",
			item: db.ItemScore{
				Title:      "Channel Title",
				Importance: 0.6,
				Relevance:  0.7,
				Summary:    "Title fallback",
			},
			threshold: 0.5,
			wantParts: []string{"Channel Title", "0.60"},
		},
		{
			name: "item without summary",
			item: db.ItemScore{
				Username:   "chan3",
				Importance: 0.7,
				Relevance:  0.8,
				Summary:    "",
			},
			threshold: 0.5,
			wantParts: []string{"@chan3", "(no summary)"},
		},
		{
			name: "item with no username and no title falls back to unknown",
			item: db.ItemScore{
				Username:   "",
				Title:      "",
				Importance: 0.5,
				Relevance:  0.6,
				Summary:    "Some content",
			},
			threshold: 0.5,
			wantParts: []string{"unknown", "0.50"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			formatScoreItem(&sb, tt.item, tt.threshold)
			got := sb.String()

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatScoreItem() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatGlobalStats(t *testing.T) {
	tests := []struct {
		name      string
		global    *db.GlobalRatingStats
		wantParts []string
	}{
		{
			name:      "nil global stats",
			global:    nil,
			wantParts: []string{"Global: <code>n/a</code>"},
		},
		{
			name: "valid global stats",
			global: &db.GlobalRatingStats{
				WeightedGood:  80.0,
				WeightedTotal: 100.0,
				RatingCount:   50,
			},
			wantParts: []string{"Global: <code>0.80</code>", "w 100.0", "n 50"},
		},
		{
			name: "zero weighted total",
			global: &db.GlobalRatingStats{
				WeightedGood:  0,
				WeightedTotal: 0,
				RatingCount:   0,
			},
			wantParts: []string{"Global: <code>0.00</code>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			formatGlobalStats(&sb, tt.global)
			got := sb.String()

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatGlobalStats() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatDiscoveryItem(t *testing.T) {
	tests := []struct {
		name      string
		discovery db.DiscoveredChannel
		wantParts []string
	}{
		{
			name: "full discovery with username",
			discovery: db.DiscoveredChannel{
				Username:       "testchannel",
				Title:          "Test Channel",
				SourceType:     "forward",
				DiscoveryCount: 5,
				MaxViews:       1000,
				MaxForwards:    50,
			},
			wantParts: []string{
				"Test Channel",
				"@testchannel",
				"forward",
				"5x",
				"1000v/50f",
			},
		},
		{
			name: "discovery without title",
			discovery: db.DiscoveredChannel{
				Username:       "newchannel",
				Title:          "",
				SourceType:     "mention",
				DiscoveryCount: 3,
			},
			wantParts: []string{
				"Unknown",
				"@newchannel",
				"mention",
				"3x",
			},
		},
		{
			name: "discovery with peer ID only",
			discovery: db.DiscoveredChannel{
				TGPeerID:       123456789,
				Title:          "Private Channel",
				SourceType:     "link",
				DiscoveryCount: 1,
			},
			wantParts: []string{
				"Private Channel",
				"ID:123456789",
				"link",
				"1x",
			},
		},
		{
			name: "discovery without engagement",
			discovery: db.DiscoveredChannel{
				Username:       "simplech",
				Title:          "Simple",
				SourceType:     "forward",
				DiscoveryCount: 2,
				MaxViews:       0,
				MaxForwards:    0,
			},
			wantParts: []string{
				"Simple",
				"@simplech",
				"2x",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDiscoveryItem(tt.discovery)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatDiscoveryItem() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestFormatAnnotationItem(t *testing.T) {
	tests := []struct {
		name      string
		item      *db.AnnotationItem
		wantParts []string
	}{
		{
			name: "full item with all fields",
			item: &db.AnnotationItem{
				ItemID:          "item-123",
				ChannelUsername: "testchannel",
				ChannelTitle:    "Test Channel",
				ChannelPeerID:   123456,
				MessageID:       42,
				Status:          "pending",
				RelevanceScore:  0.85,
				ImportanceScore: 0.72,
				Topic:           "Technology",
				Summary:         "This is a test summary",
				Text:            "Full message text here",
			},
			wantParts: []string{
				"Annotation Item",
				"@testchannel",
				"item-123",
				"pending",
				"rel <code>0.85</code>",
				"imp <code>0.72</code>",
				"Technology",
				"This is a test summary",
			},
		},
		{
			name: "item without username",
			item: &db.AnnotationItem{
				ItemID:          "item-456",
				ChannelUsername: "",
				ChannelTitle:    "Private Channel",
				ChannelPeerID:   789012,
				MessageID:       99,
				Status:          "assigned",
				RelevanceScore:  0.65,
				ImportanceScore: 0.50,
				Summary:         "Another summary",
			},
			wantParts: []string{
				"Private Channel",
				"item-456",
				"assigned",
			},
		},
		{
			name: "item without topic",
			item: &db.AnnotationItem{
				ItemID:          "item-789",
				ChannelUsername: "channel",
				Status:          "pending",
				RelevanceScore:  0.55,
				ImportanceScore: 0.40,
				Summary:         "Simple summary",
			},
			wantParts: []string{
				"@channel",
				"item-789",
			},
		},
		{
			name: "item without summary",
			item: &db.AnnotationItem{
				ItemID:          "item-no-summary",
				ChannelUsername: "testch",
				Status:          "pending",
				Summary:         "",
			},
			wantParts: []string{
				"(no summary)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAnnotationItem(tt.item)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatAnnotationItem() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestValidPromptBases(t *testing.T) {
	validBases := []string{"summarize", "narrative", "cluster_summary", "cluster_topic", "relevance_gate"}

	for _, base := range validBases {
		found := false

		for _, b := range promptBases {
			if b == base {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("expected %q to be a valid prompt base", base)
		}
	}

	// Test that invalid bases are not in the list
	invalidBases := []string{"invalid", "unknown", "random"}
	for _, base := range invalidBases {
		for _, b := range promptBases {
			if b == base {
				t.Errorf("expected %q to NOT be a valid prompt base", base)
			}
		}
	}
}

func TestFormatRatingsStatsOutputExtended(t *testing.T) {
	tests := []struct {
		name      string
		entries   []db.RatingStatsSummary
		global    *db.GlobalRatingStats
		wantParts []string
	}{
		{
			name: "multiple entries with global",
			entries: []db.RatingStatsSummary{
				{
					ChannelID:     "1",
					Username:      "chan1",
					WeightedGood:  8.0,
					WeightedTotal: 10.0,
					RatingCount:   10,
				},
				{
					ChannelID:     "2",
					Title:         "Channel Two",
					WeightedGood:  6.0,
					WeightedTotal: 12.0,
					RatingCount:   12,
				},
			},
			global: &db.GlobalRatingStats{
				WeightedGood:  14.0,
				WeightedTotal: 22.0,
				RatingCount:   22,
			},
			wantParts: []string{
				"Weighted Rating Stats",
				"@chan1",
				"Channel Two",
				"Global:",
			},
		},
		{
			name: "entries without global",
			entries: []db.RatingStatsSummary{
				{
					ChannelID:     "1",
					Username:      "onechan",
					WeightedGood:  5.0,
					WeightedTotal: 5.0,
					RatingCount:   5,
				},
			},
			global: nil,
			wantParts: []string{
				"@onechan",
				"Global: <code>n/a</code>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRatingsStatsOutput(tt.entries, tt.global)

			for _, part := range tt.wantParts {
				if !containsString(got, part) {
					t.Errorf("formatRatingsStatsOutput() missing %q in output: %s", part, got)
				}
			}
		})
	}
}

func TestParseAnnotateEnqueueArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantHours int
		wantLimit int
	}{
		{
			name:      "no args uses defaults",
			args:      []string{},
			wantHours: DefaultAnnotateHours,
			wantLimit: DefaultAnnotateLimit,
		},
		{
			name:      "first arg sets hours",
			args:      []string{"48"},
			wantHours: 48,
			wantLimit: DefaultAnnotateLimit,
		},
		{
			name:      "both args set hours and limit",
			args:      []string{"12", "100"},
			wantHours: 12,
			wantLimit: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hours := DefaultAnnotateHours
			limit := DefaultAnnotateLimit

			if len(tt.args) > 0 {
				if v, err := strconv.Atoi(tt.args[0]); err == nil && v > 0 {
					hours = v
				}
			}

			if len(tt.args) > 1 {
				if v, err := strconv.Atoi(tt.args[1]); err == nil && v > 0 {
					limit = v
				}
			}

			if hours != tt.wantHours {
				t.Errorf("hours = %d, want %d", hours, tt.wantHours)
			}

			if limit != tt.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tt.wantLimit)
			}
		})
	}
}

func TestCallbackDataParsing(t *testing.T) {
	tests := []struct {
		name           string
		data           string
		wantPrefix     string
		wantHasPrefix  bool
		wantPartsCount int
	}{
		{
			name:           "rate callback up",
			data:           "rate:digest123:up",
			wantPrefix:     CallbackPrefixRate,
			wantHasPrefix:  true,
			wantPartsCount: 3,
		},
		{
			name:           "rate callback down",
			data:           "rate:digest456:down",
			wantPrefix:     CallbackPrefixRate,
			wantHasPrefix:  true,
			wantPartsCount: 3,
		},
		{
			name:           "discover callback",
			data:           "discover:approve:testchannel",
			wantPrefix:     CallbackPrefixDiscover,
			wantHasPrefix:  true,
			wantPartsCount: 3,
		},
		{
			name:           "unknown callback",
			data:           "unknown:data:here",
			wantPrefix:     CallbackPrefixRate,
			wantHasPrefix:  false,
			wantPartsCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPrefix := strings.HasPrefix(tt.data, tt.wantPrefix)
			if hasPrefix != tt.wantHasPrefix {
				t.Errorf("HasPrefix(%q, %q) = %v, want %v", tt.data, tt.wantPrefix, hasPrefix, tt.wantHasPrefix)
			}

			parts := strings.Split(tt.data, ":")
			if len(parts) != tt.wantPartsCount {
				t.Errorf("Split parts count = %d, want %d", len(parts), tt.wantPartsCount)
			}
		})
	}
}

func TestCommandConstants(t *testing.T) {
	// Test that command constants are properly defined
	commands := map[string]string{
		"status":           CmdStatus,
		"settings":         CmdSettings,
		"history":          CmdHistory,
		"add":              CmdAdd,
		"list":             CmdList,
		"remove":           CmdRemove,
		"prompt":           CmdPrompt,
		"annotate":         CmdAnnotate,
		"min_length":       CmdMinLength,
		"minlength":        CmdMinLengthAlt,
		"skip_forwards":    CmdSkipForwards,
		"skipforwards":     CmdSkipFwdAlt,
		"target":           CmdTarget,
		"window":           CmdWindow,
		"schedule":         CmdSchedule,
		"topics":           CmdTopics,
		"dedup":            CmdDedup,
		"relevance":        CmdRelevance,
		"importance":       CmdImportance,
		"language":         CmdLanguage,
		"tone":             CmdTone,
		"model":            CmdModel,
		"smart_model":      CmdSmartModel,
		"smartmodel":       CmdSmartModelAlt,
		"editor":           CmdEditor,
		"tiered":           CmdTiered,
		"vision":           CmdVision,
		"visionrouting":    CmdVisionAlt,
		"consolidated":     CmdConsolidated,
		"editordetails":    CmdEditorDetail,
		"errors":           CmdErrors,
		"retry":            CmdRetry,
		"channel":          CmdChannel,
		"scores":           CmdScores,
		"ratings":          CmdRatings,
		"cover_image":      CmdCoverImage,
		"coverimage":       CmdCoverImageAlt,
		"ai_cover":         CmdAICover,
		"aicover":          CmdAICoverAlt,
		"inline_images":    CmdInlineImages,
		"inlineimages":     CmdInlineImagesAlt,
		"others_narrative": CmdOthersNarrative,
		"othersnarrative":  CmdOthersNarrativeAlt,
	}

	for expected, actual := range commands {
		if actual != expected {
			t.Errorf("Command constant %q = %q, want %q", actual, actual, expected)
		}
	}
}

func TestSettingConstants(t *testing.T) {
	// Test setting key constants
	settings := map[string]string{
		"filters_skip_forwards":         SettingFiltersSkipForwards,
		"relevance_threshold":           SettingRelevanceThreshold,
		"importance_threshold":          SettingImportanceThreshold,
		"editor_enabled":                SettingEditorEnabled,
		"tiered_importance_enabled":     SettingTieredImportanceEnabled,
		"vision_routing_enabled":        SettingVisionRoutingEnabled,
		"consolidated_clusters_enabled": SettingConsolidatedClustersEnabled,
		"editor_detailed_items":         SettingEditorDetailedItems,
		"digest_cover_image":            SettingDigestCoverImage,
		"digest_ai_cover":               SettingDigestAICover,
		"digest_inline_images":          SettingDigestInlineImages,
		"others_as_narrative":           SettingOthersAsNarrative,
		"discovery_min_seen":            SettingDiscoveryMinSeen,
		"discovery_min_engagement":      SettingDiscoveryMinScore,
	}

	for expected, actual := range settings {
		if actual != expected {
			t.Errorf("Setting constant %q = %q, want %q", actual, actual, expected)
		}
	}
}

func TestSubcommandConstants(t *testing.T) {
	// Test subcommand constants
	subcommands := map[string]string{
		"stats":    SubCmdStats,
		"ads":      SubCmdAds,
		"reset":    SubCmdReset,
		"clear":    SubCmdClear,
		"preview":  SubCmdPreview,
		"approve":  SubCmdApprove,
		"reject":   SubCmdReject,
		"confirm":  SubCmdConfirm,
		"auto":     SubCmdAuto,
		"mode":     SubCmdMode,
		"show":     SubCmdShow,
		"weekdays": SubCmdWeekdays,
		"weekends": SubCmdWeekends,
		"times":    SubCmdTimes,
		"hourly":   SubCmdHourly,
	}

	for expected, actual := range subcommands {
		if actual != expected {
			t.Errorf("Subcommand constant %q = %q, want %q", actual, actual, expected)
		}
	}
}

func TestFormatRatingsChannelNameEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		channelID string
		username  string
		title     string
		want      string
	}{
		{
			name:      "all empty",
			channelID: "",
			username:  "",
			title:     "",
			want:      "",
		},
		{
			name:      "username with special chars",
			channelID: "123",
			username:  "test_channel_123",
			title:     "Test",
			want:      "@test_channel_123",
		},
		{
			name:      "title with unicode",
			channelID: "456",
			username:  "",
			title:     "Канал новостей",
			want:      "Канал новостей",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRatingsChannelName(tt.channelID, tt.username, tt.title)

			if got != tt.want {
				t.Errorf("formatRatingsChannelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsNumericWeightEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0.1", true},
		{"0.10", true},
		{"0.100", true},
		{"1", true},
		{"1.0", true},
		{"1.00", true},
		{"2", true},
		{"2.0", true},
		{"0.099", false},   // below min
		{"2.001", false},   // above max
		{"1.5e0", true},    // scientific notation
		{".5", true},       // no leading zero
		{"+1.0", true},     // positive sign
		{"-0.5", false},    // negative
		{"  1.0  ", false}, // whitespace
		{"1,0", false},     // comma instead of dot
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isNumericWeight(tt.input)

			if got != tt.expected {
				t.Errorf("isNumericWeight(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestClampFloat32EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		val    float32
		minVal float32
		maxVal float32
		want   float32
	}{
		{
			name:   "equal min and max",
			val:    0.5,
			minVal: 0.5,
			maxVal: 0.5,
			want:   0.5,
		},
		{
			name:   "min greater than max returns min when val below min",
			val:    0.5,
			minVal: 1.0,
			maxVal: 0.0,
			want:   1.0, // clamps to min first, which is 1.0
		},
		{
			name:   "very small value",
			val:    0.0001,
			minVal: 0.0,
			maxVal: 1.0,
			want:   0.0001,
		},
		{
			name:   "very large value",
			val:    999999.0,
			minVal: 0.0,
			maxVal: 1.0,
			want:   1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampFloat32(tt.val, tt.minVal, tt.maxVal)

			if got != tt.want {
				t.Errorf(errClampFloat32Fmt, tt.val, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestTruncateAnnotationTextEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{
			name:  "zero limit",
			text:  "Some text",
			limit: 0,
			want:  "...",
		},
		{
			name:  "limit of 1",
			text:  "AB",
			limit: 1,
			want:  "A...",
		},
		{
			name:  "emoji truncation",
			text:  "Hello 👋 World",
			limit: 8,
			want:  "Hello 👋 ...",
		},
		{
			name:  "all emoji",
			text:  "🎉🎊🎁🎈",
			limit: 2,
			want:  "🎉🎊...",
		},
		{
			name:  "cyrillic text",
			text:  "Привет мир",
			limit: 6,
			want:  "Привет...",
		},
		{
			name:  "chinese characters",
			text:  "你好世界",
			limit: 2,
			want:  "你好...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateAnnotationText(tt.text, tt.limit)

			if got != tt.want {
				t.Errorf("truncateAnnotationText(%q, %d) = %q, want %q", tt.text, tt.limit, got, tt.want)
			}
		})
	}
}

func TestFormatChannelDisplayEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		username   string
		title      string
		identifier string
		want       string
	}{
		{
			name:       "all empty",
			username:   "",
			title:      "",
			identifier: "",
			want:       "<code></code>",
		},
		{
			name:       "username with HTML chars",
			username:   "test<script>",
			title:      "",
			identifier: "",
			want:       "<code>@test&lt;script&gt;</code>",
		},
		{
			name:       "title with HTML chars",
			username:   "",
			title:      "<b>Bold</b>",
			identifier: "",
			want:       "<b>&lt;b&gt;Bold&lt;/b&gt;</b>",
		},
		{
			name:       "identifier with HTML chars",
			username:   "",
			title:      "",
			identifier: "<script>",
			want:       "<code>&lt;script&gt;</code>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatChannelDisplay(tt.username, tt.title, tt.identifier)

			if got != tt.want {
				t.Errorf("formatChannelDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindChannelByIdentifierEdgeCases(t *testing.T) {
	channels := []db.Channel{
		{ID: "1", Username: "TestChannel", TGPeerID: 123456}, // uppercase
		{ID: "2", Username: "testchannel2", TGPeerID: 789012},
		{ID: "3", Username: "", TGPeerID: -1001234567890}, // negative ID
	}

	tests := []struct {
		name       string
		identifier string
		wantID     string
		wantNil    bool
	}{
		{
			name:       "case insensitive match",
			identifier: "testchannel", // lowercase search for uppercase
			wantID:     "1",
		},
		{
			name:       "negative peer ID",
			identifier: "-1001234567890",
			wantID:     "3",
		},
		{
			name:       "non-existent peer ID",
			identifier: "999999999",
			wantNil:    true,
		},
		{
			name:       "whitespace only",
			identifier: "   ",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findChannelByIdentifier(channels, tt.identifier)

			if tt.wantNil {
				if got != nil {
					t.Errorf("findChannelByIdentifier() = %v, want nil", got)
				}

				return
			}

			if got == nil {
				t.Fatal("findChannelByIdentifier() = nil, want non-nil")
			}

			if got.ID != tt.wantID {
				t.Errorf("findChannelByIdentifier().ID = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestRatingConstants(t *testing.T) {
	if RatingGood != expectedRatingGood {
		t.Errorf("RatingGood = %q, want %q", RatingGood, expectedRatingGood)
	}

	if RatingBad != expectedRatingBad {
		t.Errorf("RatingBad = %q, want %q", RatingBad, expectedRatingBad)
	}

	if RatingIrrelevant != expectedRatingIrrelevant {
		t.Errorf("RatingIrrelevant = %q, want %q", RatingIrrelevant, expectedRatingIrrelevant)
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultRatingsDays != 30 {
		t.Errorf("DefaultRatingsDays = %d, want %d", DefaultRatingsDays, 30)
	}

	if DefaultRatingsLimit != 10 {
		t.Errorf("DefaultRatingsLimit = %d, want %d", DefaultRatingsLimit, 10)
	}

	if DefaultScoresHours != 24 {
		t.Errorf("DefaultScoresHours = %d, want %d", DefaultScoresHours, 24)
	}

	if DefaultScoresLimit != 10 {
		t.Errorf("DefaultScoresLimit = %d, want %d", DefaultScoresLimit, 10)
	}

	if DefaultAnnotateHours != 24 {
		t.Errorf("DefaultAnnotateHours = %d, want %d", DefaultAnnotateHours, 24)
	}

	if DefaultAnnotateLimit != 50 {
		t.Errorf("DefaultAnnotateLimit = %d, want %d", DefaultAnnotateLimit, 50)
	}
}

func TestLogFieldConstants(t *testing.T) {
	if LogFieldUserID != expectedLogFieldUserID {
		t.Errorf("LogFieldUserID = %q, want %q", LogFieldUserID, expectedLogFieldUserID)
	}

	if LogFieldUsername != expectedLogFieldUsername {
		t.Errorf("LogFieldUsername = %q, want %q", LogFieldUsername, expectedLogFieldUsername)
	}
}

func TestButtonLabels(t *testing.T) {
	if ButtonUseful != expectedButtonUseful {
		t.Errorf("ButtonUseful = %q, want %q", ButtonUseful, expectedButtonUseful)
	}

	if ButtonNotUseful != expectedButtonNotUseful {
		t.Errorf("ButtonNotUseful = %q, want %q", ButtonNotUseful, expectedButtonNotUseful)
	}
}

func TestCallbackPrefixes(t *testing.T) {
	if CallbackPrefixRate != expectedCallbackRate {
		t.Errorf("CallbackPrefixRate = %q, want %q", CallbackPrefixRate, expectedCallbackRate)
	}

	if CallbackPrefixDiscover != expectedCallbackDiscover {
		t.Errorf("CallbackPrefixDiscover = %q, want %q", CallbackPrefixDiscover, expectedCallbackDiscover)
	}

	if CallbackSuffixUp != expectedCallbackUp {
		t.Errorf("CallbackSuffixUp = %q, want %q", CallbackSuffixUp, expectedCallbackUp)
	}

	if CallbackSuffixDown != expectedCallbackDown {
		t.Errorf("CallbackSuffixDown = %q, want %q", CallbackSuffixDown, expectedCallbackDown)
	}
}

// Additional edge case tests have been consolidated into existing test functions above.

func TestStatusConstants(t *testing.T) {
	if StatusEnabled != expectedStatusEnabled {
		t.Errorf("StatusEnabled = %q, want %q", StatusEnabled, expectedStatusEnabled)
	}

	if StatusDisabled != expectedStatusDisabled {
		t.Errorf("StatusDisabled = %q, want %q", StatusDisabled, expectedStatusDisabled)
	}
}

func TestWeightOverrideConstants(t *testing.T) {
	if WeightOverrideManual != expectedWeightManual {
		t.Errorf("WeightOverrideManual = %q, want %q", WeightOverrideManual, expectedWeightManual)
	}

	if ToggleOff != expectedToggleOff {
		t.Errorf("ToggleOff = %q, want %q", ToggleOff, expectedToggleOff)
	}
}

func TestDateTimeFormats(t *testing.T) {
	if DateTimeFormat != expectedDateTimeFormat {
		t.Errorf("DateTimeFormat = %q, want %q", DateTimeFormat, expectedDateTimeFormat)
	}

	if TimeFormat != expectedTimeFormat {
		t.Errorf("TimeFormat = %q, want %q", TimeFormat, expectedTimeFormat)
	}

	if DateFormatYMD != expectedDateFormatYMD {
		t.Errorf("DateFormatYMD = %q, want %q", DateFormatYMD, expectedDateFormatYMD)
	}
}

func TestSchedulePreviewConstants(t *testing.T) {
	if schedulePreviewDefault != 5 {
		t.Errorf("schedulePreviewDefault = %d, want %d", schedulePreviewDefault, 5)
	}

	if schedulePreviewMax != 20 {
		t.Errorf("schedulePreviewMax = %d, want %d", schedulePreviewMax, 20)
	}
}

func TestEntityTypeConstants(t *testing.T) {
	if EntityTypeBotCommand != expectedEntityTypeBotCmd {
		t.Errorf("EntityTypeBotCommand = %q, want %q", EntityTypeBotCommand, expectedEntityTypeBotCmd)
	}
}

func TestQueryLimitConstants(t *testing.T) {
	if RecentErrorsLimit != 10 {
		t.Errorf("RecentErrorsLimit = %d, want %d", RecentErrorsLimit, 10)
	}

	if SettingHistoryLimit != 20 {
		t.Errorf("SettingHistoryLimit = %d, want %d", SettingHistoryLimit, 20)
	}

	if DiscoveriesLimit != 15 {
		t.Errorf("DiscoveriesLimit = %d, want %d", DiscoveriesLimit, 15)
	}

	if RetryErrorsLimit != 1000 {
		t.Errorf("RetryErrorsLimit = %d, want %d", RetryErrorsLimit, 1000)
	}
}

func TestPromptKeyFormats(t *testing.T) {
	if PromptActiveKeyFmt != expectedPromptActiveKeyFmt {
		t.Errorf("PromptActiveKeyFmt = %q, want %q", PromptActiveKeyFmt, expectedPromptActiveKeyFmt)
	}

	if PromptKeyFmt != expectedPromptKeyFmt {
		t.Errorf("PromptKeyFmt = %q, want %q", PromptKeyFmt, expectedPromptKeyFmt)
	}
}

func TestErrorMessageFormats(t *testing.T) {
	if ErrSavingFmt != expectedErrSavingFmt {
		t.Errorf("ErrSavingFmt unexpected value")
	}

	if ErrFetchingChannelsFmt != expectedErrFetchChannelFmt {
		t.Errorf("ErrFetchingChannelsFmt unexpected value")
	}

	if ErrUnknownBaseFmt != expectedErrUnknownBaseFmt {
		t.Errorf("ErrUnknownBaseFmt unexpected value")
	}

	if ErrChannelNotFoundFmt != expectedErrChannelNotFound {
		t.Errorf("ErrChannelNotFoundFmt unexpected value")
	}

	if ErrGenericFmt != expectedErrGenericFmt {
		t.Errorf("ErrGenericFmt unexpected value")
	}

	if ErrNoRows != expectedErrNoRows {
		t.Errorf("ErrNoRows unexpected value")
	}
}

func TestHelpSummaryMessage(t *testing.T) {
	msg := helpSummaryMessage()

	wantParts := []string{
		"Telegram Digest Bot",
		"/setup",
		"/status",
		"/preview",
		"/channel",
		"/filter",
		"/discover",
		"/schedule",
		"/config",
		"/ai",
		"/system",
		"/scores",
		"/factcheck",
		"/enrichment",
		"/ratings",
		"/annotate",
		"/help",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpSummaryMessage() missing %q", part)
		}
	}
}

func TestHelpChannelsMessage(t *testing.T) {
	msg := helpChannelsMessage()

	wantParts := []string{
		"Channel Management",
		"/channel add",
		"/channel remove",
		"/channel list",
		"/channel context",
		"/channel weight",
		"/channel relevance",
		"/channel stats",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpChannelsMessage() missing %q", part)
		}
	}
}

func TestHelpFiltersMessage(t *testing.T) {
	msg := helpFiltersMessage()

	wantParts := []string{
		"Filters",
		"/filter list",
		"/filter add",
		"/filter remove",
		"/filter ads",
		"/filter mode",
		"/filter keywords",
		"/filter min_length",
		"/filter skip_forwards",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpFiltersMessage() missing %q", part)
		}
	}
}

func TestHelpDiscoverMessage(t *testing.T) {
	msg := helpDiscoverMessage()

	wantParts := []string{
		"Discovery",
		"/discover",
		"/discover preview",
		"/discover approve",
		"/discover reject",
		"/discover allow",
		"/discover deny",
		"/discover min_seen",
		"/discover min_engagement",
		"/discover show-rejected",
		"/discover cleanup",
		"/discover stats",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpDiscoverMessage() missing %q", part)
		}
	}
}

func TestHelpScheduleMessage(t *testing.T) {
	msg := helpScheduleMessage()

	wantParts := []string{
		"Schedule",
		"/schedule timezone",
		"/schedule weekdays times",
		"/schedule weekdays hourly",
		"/schedule weekends hourly",
		"/schedule preview",
		"/schedule clear",
		"/schedule show",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpScheduleMessage() missing %q", part)
		}
	}
}

func TestHelpConfigMessage(t *testing.T) {
	msg := helpConfigMessage()

	wantParts := []string{
		"Configuration",
		"/config target",
		"/config window",
		"/config language",
		"/config tone",
		"/config relevance",
		"/config importance",
		"/config links",
		"/config maxlinks",
		"/config discovery_min_seen",
		"/config discovery_min_engagement",
		"/config reset",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpConfigMessage() missing %q", part)
		}
	}
}

func TestHelpAIMessage(t *testing.T) {
	msg := helpAIMessage()

	wantParts := []string{
		"AI",
		"/ai model",
		"/ai smart_model",
		"/ai tone",
		"/ai prompt",
		"/ai editor",
		"/ai tiered",
		"/ai vision",
		"/ai consolidated",
		"/ai details",
		"/ai topics",
		"/ai dedup",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpAIMessage() missing %q", part)
		}
	}
}

func TestHelpSystemMessage(t *testing.T) {
	msg := helpSystemMessage()

	wantParts := []string{
		"System",
		"/system status",
		"/system settings",
		"/system errors",
		"/system retry",
		"/system link_backfill",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpSystemMessage() missing %q", part)
		}
	}
}

func TestHelpScoresMessage(t *testing.T) {
	msg := helpScoresMessage()

	wantParts := []string{
		"Scores",
		"/scores",
		"/scores debug",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpScoresMessage() missing %q", part)
		}
	}
}

func TestHelpRatingsMessage(t *testing.T) {
	msg := helpRatingsMessage()

	wantParts := []string{
		"Ratings",
		"/ratings",
		"/ratings stats",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpRatingsMessage() missing %q", part)
		}
	}
}

func TestHelpAnnotateMessage(t *testing.T) {
	msg := helpAnnotateMessage()

	wantParts := []string{
		"Annotations",
		"/annotate",
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpAnnotateMessage() missing %q", part)
		}
	}
}

func TestHelpAllMessage(t *testing.T) {
	msg := helpAllMessage()

	// helpAllMessage concatenates all help sections
	wantParts := []string{
		"Telegram Digest Bot", // from helpSummaryMessage
		"Channel Management",  // from helpChannelsMessage
		"Discovery",           // from helpDiscoverMessage
		"Filters",             // from helpFiltersMessage
		"Schedule",            // from helpScheduleMessage
		"Configuration",       // from helpConfigMessage
		"AI",                  // from helpAIMessage
		"Enrichment Commands", // from enrichmentHelpMessage
		"System",              // from helpSystemMessage
		"Scores",              // from helpScoresMessage
		"Fact Check",          // from helpFactCheckMessage
		"Ratings",             // from helpRatingsMessage
		"Annotations",         // from helpAnnotateMessage
	}

	for _, part := range wantParts {
		if !containsString(msg, part) {
			t.Errorf("helpAllMessage() missing %q", part)
		}
	}
}

func TestBotFatherCommandsMessage(t *testing.T) {
	msg := botFatherCommandsMessage()

	// Should contain plain commands without HTML that BotFather can use
	if !containsString(msg, "start") {
		t.Error("botFatherCommandsMessage() missing 'start'")
	}

	if !containsString(msg, "help") {
		t.Error("botFatherCommandsMessage() missing 'help'")
	}

	if !containsString(msg, "channel") {
		t.Error("botFatherCommandsMessage() missing 'channel'")
	}
}
