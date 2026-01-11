package htmlutils

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf16"
)

// utf16Len returns the number of UTF-16 code units needed to encode the string.
// Telegram counts message length in UTF-16 code units, not Unicode code points.
// Characters outside the BMP (emoji, etc.) require surrogate pairs (2 code units).
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// utf16Slice safely slices a string by UTF-16 code unit count.
// It returns the portion of the string that fits within the specified UTF-16 length.
func utf16Slice(s string, maxUnits int) string {
	runes := []rune(s)
	units := 0
	for i, r := range runes {
		runeUnits := 1
		if r > 0xFFFF {
			runeUnits = 2 // Surrogate pair needed
		}
		if units+runeUnits > maxUnits {
			return string(runes[:i])
		}
		units += runeUnits
	}
	return s
}

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
// For <a> tags, only safe href attributes are preserved. All other tags have attributes stripped.
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
			isClosing := matches[1] == "/"
			tagName := strings.ToLower(matches[2])
			if allowedTags[tagName] {
				if tagName == "a" && !isClosing {
					// Sanitize <a> tag - only allow safe href
					sanitizedTag := sanitizeAnchorTag(tag)
					sb.WriteString(sanitizedTag)
				} else {
					// Strip all attributes from non-<a> tags (Telegram doesn't support them)
					if isClosing {
						sb.WriteString("</" + tagName + ">")
					} else {
						sb.WriteString("<" + tagName + ">")
					}
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

// Item boundary markers for intelligent splitting
// These are stripped before sending to Telegram
const (
	ItemStart = "<!-- ITEM -->"
	ItemEnd   = "<!-- /ITEM -->"
)

// StripItemMarkers removes item boundary markers from text before sending to Telegram
func StripItemMarkers(text string) string {
	text = strings.ReplaceAll(text, ItemStart, "")
	text = strings.ReplaceAll(text, ItemEnd, "")
	return text
}

// splitAfter defines markers where we split AFTER the marker (marker stays in current part)
var splitAfter = []string{
	ItemEnd + "\n",              // Highest priority: between complete items
	"</blockquote>\n",           // After blockquote end - prefer split here
	"━━━━━━━━━━━━━━━━━━━━━━\n",  // Section separator
	"─────────────────────\n",  // Sub-section separator
	"\n\n",                     // Paragraph break
	"\n    ↳ ",                  // Source attribution line (complete before splitting)
}

// splitBefore defines markers where we split BEFORE the content (only newline stays in current part)
// These are section headers that should start the next message, not end the current one
var splitBefore = []string{
	"\n🔴 ",                    // Breaking section - split before emoji
	"\n📌 ",                    // Notable section - split before emoji
	"\n📝 ",                    // Also section - split before emoji
	"\n┌─────────────────────", // Topic header - split before box
}

// SplitHTML splits an HTML string into multiple parts, each within the specified limit.
// It tries to split at semantic boundaries (sections, paragraphs) before falling back to lines.
// The limit is in UTF-16 code units, matching Telegram's message length counting.
func SplitHTML(text string, limit int) []string {
	var parts []string
	var current strings.Builder
	var openTags []string
	currentLen := 0 // UTF-16 code units

	type token struct {
		val      string
		isTag    bool
		isMarker bool // Special marker tokens (not counted, used for split priority)
	}
	var tokens []token

	// Tokenize: find all HTML tags and item markers
	// Item markers are treated like tags (not counted toward limit)
	remaining := text
	for len(remaining) > 0 {
		// Check for item markers first
		if strings.HasPrefix(remaining, ItemStart) {
			tokens = append(tokens, token{val: ItemStart, isTag: true, isMarker: true})
			remaining = remaining[len(ItemStart):]
			continue
		}
		if strings.HasPrefix(remaining, ItemEnd) {
			tokens = append(tokens, token{val: ItemEnd, isTag: true, isMarker: true})
			remaining = remaining[len(ItemEnd):]
			continue
		}

		// Check for HTML tags
		tagMatch := tagRegex.FindStringIndex(remaining)
		if tagMatch != nil && tagMatch[0] == 0 {
			tokens = append(tokens, token{val: remaining[:tagMatch[1]], isTag: true})
			remaining = remaining[tagMatch[1]:]
			continue
		}

		// Find next tag or marker
		nextTag := len(remaining)
		if tagMatch != nil {
			nextTag = tagMatch[0]
		}
		nextStart := strings.Index(remaining, ItemStart)
		if nextStart >= 0 && nextStart < nextTag {
			nextTag = nextStart
		}
		nextEnd := strings.Index(remaining, ItemEnd)
		if nextEnd >= 0 && nextEnd < nextTag {
			nextTag = nextEnd
		}

		// Add text content up to next tag/marker
		if nextTag > 0 {
			tokens = append(tokens, token{val: remaining[:nextTag], isTag: false})
			remaining = remaining[nextTag:]
		} else {
			// Shouldn't happen, but handle gracefully
			tokens = append(tokens, token{val: remaining, isTag: false})
			break
		}
	}

	// Count total UTF-16 code units in text content
	totalLen := 0
	for _, t := range tokens {
		if !t.isTag {
			totalLen += utf16Len(t.val)
		}
	}
	if totalLen <= limit {
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
		currentLen = 0

		// Filter openTags: remove noReopenTags (they were closed and won't be reopened)
		// This prevents closing them again in subsequent parts
		var newOpenTags []string
		for _, tag := range openTags {
			tagName := strings.ToLower(GetTagName(tag))
			if !noReopenTags[tagName] {
				newOpenTags = append(newOpenTags, tag)
				current.WriteString(tag)
			}
		}
		openTags = newOpenTags
	}

	// findBestSplit finds the best position to split within the given text chunk.
	// maxUnits is in UTF-16 code units. Returns the string to write and the remainder.
	findBestSplit := func(text string, maxUnits int) (toWrite, remainder string) {
		textLen := utf16Len(text)
		if textLen <= maxUnits {
			return text, ""
		}

		// Get the searchable portion by UTF-16 length
		searchText := utf16Slice(text, maxUnits)

		// Try "split after" markers first (marker stays in current part)
		for _, sep := range splitAfter {
			pos := strings.LastIndex(searchText, sep)
			if pos > 0 {
				splitAt := pos + len(sep)
				return searchText[:splitAt], text[splitAt:]
			}
		}

		// Try "split before" markers (only newline stays, rest goes to next part)
		for _, sep := range splitBefore {
			pos := strings.LastIndex(searchText, sep)
			if pos > 0 {
				// Split after the newline only, marker content goes to next part
				splitAt := pos + 1 // Just the \n
				return searchText[:splitAt], text[splitAt:]
			}
		}

		// Try to split at a newline
		pos := strings.LastIndex(searchText, "\n")
		if pos > 0 {
			return searchText[:pos+1], text[pos+1:]
		}

		// Try to split at a space (word boundary) - always use if found
		pos = strings.LastIndex(searchText, " ")
		if pos > 0 {
			return searchText[:pos+1], text[pos+1:]
		}

		// Last resort: split at maxUnits (utf16Slice ensures we don't split mid-character)
		return searchText, text[len(searchText):]
	}

	for i, t := range tokens {
		if t.isTag {
			if !t.isMarker {
				// Check if this is a closing tag for a noReopenTag that was already closed
				matches := tagRegex.FindStringSubmatch(t.val)
				if len(matches) >= 3 && matches[1] == "/" {
					tagName := strings.ToLower(matches[2])
					if noReopenTags[tagName] {
						// Check if this tag is currently open
						found := false
						for _, ot := range openTags {
							if strings.ToLower(GetTagName(ot)) == tagName {
								found = true
								break
							}
						}
						if !found {
							// This closing tag was already emitted in a previous flush, skip it
							continue
						}
					}
				}
				openTags = updateOpenTags(t.val, openTags)
			}
			current.WriteString(t.val)
			// Prefer splitting at ItemEnd boundaries when approaching limit
			if t.val == ItemEnd {
				// Check if next token is a newline, and if we're past 50% of limit
				// If so, flush now to split at item boundary
				if currentLen > limit/2 && i+1 < len(tokens) {
					nextToken := tokens[i+1]
					if !nextToken.isTag && strings.HasPrefix(nextToken.val, "\n") {
						// Write the newline to current, then flush
						current.WriteString("\n")
						flush()
						// Skip the newline from next token
						tokens[i+1] = token{val: strings.TrimPrefix(nextToken.val, "\n"), isTag: false}
						continue
					}
				}
			}
		} else {
			remaining := t.val
			for len(remaining) > 0 {
				canTake := limit - currentLen
				if canTake <= 0 {
					flush()
					canTake = limit
				}

				remainingLen := utf16Len(remaining)
				if remainingLen <= canTake {
					current.WriteString(remaining)
					currentLen += remainingLen
					remaining = ""
				} else {
					// Find the best split point
					toWrite, newRemaining := findBestSplit(remaining, canTake)

					if len(toWrite) > 0 {
						current.WriteString(toWrite)
						currentLen += utf16Len(toWrite)
						remaining = strings.TrimLeft(newRemaining, " \t\n\r")
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
