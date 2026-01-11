package htmlutils

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

var tagRegex = regexp.MustCompile(`<(/?)([a-zA-Z0-9-]+)([^>]*)>`)
var hrefRegex = regexp.MustCompile(`(?i)\s*href\s*=\s*["']([^"']*)["']`)

var allowedTags = map[string]bool{
	"b":          true,
	"i":          true,
	"u":          true,
	"s":          true,
	"code":       true,
	"pre":        true,
	"a":          true,
	"blockquote": true,
	"tg-spoiler": true,
}

// dangerousProtocols lists URL protocols that should be stripped
var dangerousProtocols = []string{
	"javascript:",
	"vbscript:",
	"data:",
}

// SanitizeHTML ensures only Telegram-supported HTML tags are kept and text is properly escaped.
// For <a> tags, only safe href attributes are preserved.
func SanitizeHTML(text string) string {
	var sb strings.Builder
	indices := tagRegex.FindAllStringIndex(text, -1)
	lastPos := 0
	for _, idx := range indices {
		if idx[0] > lastPos {
			sb.WriteString(html.EscapeString(text[lastPos:idx[0]]))
		}

		tag := text[idx[0]:idx[1]]
		matches := tagRegex.FindStringSubmatch(tag)
		if len(matches) >= 3 {
			tagName := strings.ToLower(matches[2])
			if allowedTags[tagName] {
				if tagName == "a" && matches[1] != "/" {
					// Sanitize <a> tag - only allow safe href
					sanitizedTag := sanitizeAnchorTag(tag)
					sb.WriteString(sanitizedTag)
				} else {
					sb.WriteString(tag)
				}
			}
			// Strip unsupported tags but keep content
		}

		lastPos = idx[1]
	}
	if lastPos < len(text) {
		sb.WriteString(html.EscapeString(text[lastPos:]))
	}
	return sb.String()
}

// sanitizeAnchorTag sanitizes an <a> tag to only include safe href
func sanitizeAnchorTag(tag string) string {
	hrefMatch := hrefRegex.FindStringSubmatch(tag)
	if hrefMatch == nil {
		return "<a>" // No href found
	}

	href := hrefMatch[1]
	hrefLower := strings.ToLower(strings.TrimSpace(href))

	// Check for dangerous protocols
	for _, proto := range dangerousProtocols {
		if strings.HasPrefix(hrefLower, proto) {
			return "<a>" // Strip dangerous href
		}
	}

	// Return sanitized tag with only href attribute
	return `<a href="` + html.EscapeString(href) + `">`
}

// splitPriority defines preferred split points in order of preference
var splitPriorities = []string{
	"</blockquote>\n",           // After blockquote end - prefer split here
	"━━━━━━━━━━━━━━━━━━━━━━\n",  // Section separator
	"─────────────────────\n",  // Sub-section separator
	"\n\n",                     // Paragraph break
	"\n🔴 ",                    // Breaking section
	"\n📌 ",                    // Notable section
	"\n📝 ",                    // Also section
	"\n┌─────────────────────\n", // Topic header
	"\n    ↳ ",                  // Source attribution line (complete before splitting)
}

// SplitHTML splits an HTML string into multiple parts, each within the specified limit.
// It tries to split at semantic boundaries (sections, paragraphs) before falling back to lines.
// The limit is in runes (Unicode code points), not bytes, to properly handle non-ASCII text.
func SplitHTML(text string, limit int) []string {
	var parts []string
	var current strings.Builder
	var openTags []string
	currentRuneLen := 0

	type token struct {
		val   string
		isTag bool
	}
	var tokens []token

	indices := tagRegex.FindAllStringIndex(text, -1)
	lastPos := 0
	for _, idx := range indices {
		if idx[0] > lastPos {
			tokens = append(tokens, token{val: text[lastPos:idx[0]], isTag: false})
		}
		tokens = append(tokens, token{val: text[idx[0]:idx[1]], isTag: true})
		lastPos = idx[1]
	}
	if lastPos < len(text) {
		tokens = append(tokens, token{val: text[lastPos:], isTag: false})
	}

	// Count total runes (not bytes) in text content
	totalRuneLen := 0
	for _, t := range tokens {
		if !t.isTag {
			totalRuneLen += utf8.RuneCountInString(t.val)
		}
	}
	if totalRuneLen <= limit {
		return []string{text}
	}

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tagsLen := 0
		for _, tag := range openTags {
			tagsLen += len(tag)
		}
		if current.Len() <= tagsLen {
			return
		}

		content := current.String()
		// Trim trailing whitespace before closing tags
		content = strings.TrimRight(content, " \t")
		// Close all open tags in reverse order
		for i := len(openTags) - 1; i >= 0; i-- {
			content += "</" + GetTagName(openTags[i]) + ">"
		}
		parts = append(parts, content)

		current.Reset()
		currentRuneLen = 0
		// Reopen tags that should be reopened
		for _, tag := range openTags {
			tagName := strings.ToLower(GetTagName(tag))
			if !noReopenTags[tagName] {
				current.WriteString(tag)
			}
		}
	}

	// runeSlice safely slices a string by rune count, never splitting a rune
	runeSlice := func(s string, start, end int) string {
		runes := []rune(s)
		if start > len(runes) {
			start = len(runes)
		}
		if end > len(runes) {
			end = len(runes)
		}
		if start > end {
			start = end
		}
		return string(runes[start:end])
	}

	// findBestSplit finds the best position to split within the given text chunk
	// Returns the rune position to split at
	findBestSplit := func(text string, maxRunes int) int {
		runeCount := utf8.RuneCountInString(text)
		if runeCount <= maxRunes {
			return runeCount
		}

		// Get the searchable portion as runes
		searchText := runeSlice(text, 0, maxRunes)

		// Try each priority split point
		for _, sep := range splitPriorities {
			pos := strings.LastIndex(searchText, sep)
			if pos > 0 {
				// Convert byte position to rune position
				runePos := utf8.RuneCountInString(searchText[:pos])
				return runePos + utf8.RuneCountInString(sep)
			}
		}

		// Try to split at a newline
		pos := strings.LastIndex(searchText, "\n")
		if pos > 0 {
			runePos := utf8.RuneCountInString(searchText[:pos])
			return runePos + 1
		}

		// Try to split at a space (word boundary) - always use if found
		pos = strings.LastIndex(searchText, " ")
		if pos > 0 {
			runePos := utf8.RuneCountInString(searchText[:pos])
			return runePos + 1
		}

		// Last resort: split at maxRunes (safe, won't split mid-rune)
		return maxRunes
	}

	for _, t := range tokens {
		if t.isTag {
			current.WriteString(t.val)
			openTags = updateOpenTags(t.val, openTags)
		} else {
			remaining := t.val
			for len(remaining) > 0 {
				canTake := limit - currentRuneLen
				if canTake <= 0 {
					flush()
					canTake = limit
				}

				remainingRunes := utf8.RuneCountInString(remaining)
				if remainingRunes <= canTake {
					current.WriteString(remaining)
					currentRuneLen += remainingRunes
					remaining = ""
				} else {
					// Find the best split point (in runes)
					splitPos := findBestSplit(remaining, canTake)

					if splitPos > 0 {
						toWrite := runeSlice(remaining, 0, splitPos)
						current.WriteString(toWrite)
						currentRuneLen += splitPos
						remaining = runeSlice(remaining, splitPos, remainingRunes)
						// Trim leading whitespace from next part
						remaining = strings.TrimLeft(remaining, " \t\n\r")
					}

					if len(remaining) > 0 {
						flush()
					}
				}
			}
		}
	}
	flush()
	return parts
}

func GetTagName(fullTag string) string {
	tag := strings.Trim(fullTag, "<>")
	parts := strings.Fields(tag)
	if len(parts) > 0 {
		return strings.TrimPrefix(parts[0], "/")
	}
	return ""
}

// Tags that should NOT be reopened across message parts
// because they have context-specific content (headers, titles)
// Note: These tags are still properly CLOSED when flushing, just not reopened
var noReopenTags = map[string]bool{
	"blockquote": true, // Blockquote has "Overview" header, shouldn't be reopened mid-content
}

func updateOpenTags(line string, openTags []string) []string {
	matches := tagRegex.FindAllStringSubmatch(line, -1)
	for _, match := range matches {
		isClosing := match[1] == "/"
		tagName := strings.ToLower(match[2])

		if isClosing {
			if len(openTags) > 0 {
				for i := len(openTags) - 1; i >= 0; i-- {
					if strings.ToLower(GetTagName(openTags[i])) == tagName {
						openTags = openTags[:i]
						break
					}
				}
			}
		} else {
			// Track ALL tags for proper closing, including noReopenTags
			openTags = append(openTags, match[0])
		}
	}
	return openTags
}
