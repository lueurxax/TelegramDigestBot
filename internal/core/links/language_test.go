package links

import "testing"

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{name: "English", text: "Apple Inc announced new iPhone sales increased by 15%", expected: langEnglish},
		{name: "Russian", text: "Президент объявил о новых мерах поддержки экономики", expected: langRussian},
		{name: "Ukrainian", text: "Президент України підписав закон про освіту", expected: langUkrainian},
		{name: "Greek", text: "Η κυβέρνηση ανακοίνωσε νέα μέτρα για την οικονομία", expected: langGreek},
		{name: "Latin non-English", text: "Guten Tag aus Berlin und willkommen", expected: ""},
		{name: "Numbers only", text: "12345 67890", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectLanguage(tt.text)
			if got != tt.expected {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}
