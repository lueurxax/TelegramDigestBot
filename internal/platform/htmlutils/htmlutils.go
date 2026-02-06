// Package htmlutils provides HTML processing utilities for Telegram messages.
//
// The package handles:
//   - UTF-16 length calculation (Telegram's native encoding)
//   - Safe string slicing by UTF-16 code units
//   - HTML entity encoding/decoding
//   - Tag stripping and sanitization
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

const emptyAnchorTag = "<a>"

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
// Unclosed tags are automatically closed at the end to prevent HTML parse errors.
func SanitizeHTML(text string) string {
	var sb strings.Builder

	var openTags []string

	indices := tagRegex.FindAllStringIndex(text, -1)
	lastPos := 0

	for _, idx := range indices {
		if idx[0] > lastPos {
			sb.WriteString(html.EscapeString(text[lastPos:idx[0]]))
		}

		tag := text[idx[0]:idx[1]]
		openTags = processHTMLTag(&sb, tag, openTags)
		lastPos = idx[1]
	}

	if lastPos < len(text) {
		sb.WriteString(html.EscapeString(text[lastPos:]))
	}

	closeUnclosedTags(&sb, openTags)

	return sb.String()
}

// processHTMLTag processes a single HTML tag and updates the open tags stack.
func processHTMLTag(sb *strings.Builder, tag string, openTags []string) []string {
	matches := tagRegex.FindStringSubmatch(tag)
	if len(matches) < 3 {
		return openTags
	}

	isClosing := matches[1] == "/"
	tagName := strings.ToLower(matches[2])

	if !allowedTags[tagName] {
		return openTags
	}

	return writeAllowedTag(sb, tag, tagName, isClosing, openTags)
}

// writeAllowedTag writes an allowed tag to the builder and updates open tags.
func writeAllowedTag(sb *strings.Builder, tag, tagName string, isClosing bool, openTags []string) []string {
	if tagName == "a" && !isClosing {
		sanitizedTag := sanitizeAnchorTag(tag)
		sb.WriteString(sanitizedTag)

		return append(openTags, tagName)
	}

	if isClosing {
		return writeClosingTag(sb, tagName, openTags)
	}

	sb.WriteString("<" + tagName + ">")

	return append(openTags, tagName)
}

// writeClosingTag writes a closing tag only if there's a matching open tag.
func writeClosingTag(sb *strings.Builder, tagName string, openTags []string) []string {
	idx := findLastOpenTag(openTags, tagName)
	if idx < 0 {
		return openTags
	}

	sb.WriteString("</" + tagName + ">")

	return openTags[:idx]
}

// closeUnclosedTags closes any unclosed tags in reverse order.
func closeUnclosedTags(sb *strings.Builder, openTags []string) {
	for i := len(openTags) - 1; i >= 0; i-- {
		sb.WriteString("</" + openTags[i] + ">")
	}
}

// findLastOpenTag finds the last occurrence of a tag name in the open tags stack.
func findLastOpenTag(openTags []string, tagName string) int {
	for i := len(openTags) - 1; i >= 0; i-- {
		if openTags[i] == tagName {
			return i
		}
	}

	return -1
}

// sanitizeAnchorTag sanitizes an <a> tag to only include safe href
func sanitizeAnchorTag(tag string) string {
	hrefMatch := hrefRegex.FindStringSubmatch(tag)
	if hrefMatch == nil {
		return emptyAnchorTag // No href found
	}

	href := hrefMatch[1]
	hrefLower := strings.ToLower(strings.TrimSpace(href))

	// Check for dangerous protocols
	for _, proto := range dangerousProtocols {
		if strings.HasPrefix(hrefLower, proto) {
			return emptyAnchorTag // Strip dangerous href
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

// StripHTMLTags removes all HTML tags from text, keeping only the content.
func StripHTMLTags(text string) string {
	result := tagRegex.ReplaceAllString(text, "")
	result = html.UnescapeString(result)

	return strings.TrimSpace(result)
}

// splitAfter defines markers where we split AFTER the marker (marker stays in current part)
var splitAfter = []string{
	ItemEnd + "\n",    // Highest priority: between complete items
	"</blockquote>\n", // After blockquote end - prefer split here
	"━━━━━━━━━━━━━━━━━━━━━━\n", // Section separator
	"─────────────────────\n",  // Sub-section separator
	"\n\n",     // Paragraph break
	"\n    ↳ ", // Source attribution line (complete before splitting)
}

// splitBefore defines markers where we split BEFORE the content (only newline stays in current part)
// These are section headers that should start the next message, not end the current one
var splitBefore = []string{
	"\n🔴 ", // Breaking section - split before emoji
	"\n📌 ", // Notable section - split before emoji
	"\n📝 ", // Also section - split before emoji
	"\n┌─────────────────────", // Topic header - split before box
}

// SplitHTML splits an HTML string into multiple parts, each within the specified limit.
// It tries to split at semantic boundaries (sections, paragraphs) before falling back to lines.
// The limit is in UTF-16 code units, matching Telegram's message length counting.
func SplitHTML(text string, limit int) []string {
	tokens := tokenizeHTML(text)

	if calculateTotalTextLen(tokens) <= limit {
		return []string{text}
	}

	splitter := newHTMLSplitter(limit)
	splitter.processTokens(tokens)

	return splitter.parts
}

type htmlToken struct {
	val      string
	isTag    bool
	isMarker bool
}

type htmlSplitter struct {
	parts      []string
	current    strings.Builder
	openTags   []string
	currentLen int
	limit      int
}

func newHTMLSplitter(limit int) *htmlSplitter {
	return &htmlSplitter{limit: limit}
}

func tokenizeHTML(text string) []htmlToken {
	var tokens []htmlToken

	remaining := text
	for len(remaining) > 0 {
		tok, consumed := parseNextToken(remaining)
		tokens = append(tokens, tok)
		remaining = remaining[consumed:]

		if consumed == 0 {
			break
		}
	}

	return tokens
}

func parseNextToken(remaining string) (htmlToken, int) {
	if strings.HasPrefix(remaining, ItemStart) {
		return htmlToken{val: ItemStart, isTag: true, isMarker: true}, len(ItemStart)
	}

	if strings.HasPrefix(remaining, ItemEnd) {
		return htmlToken{val: ItemEnd, isTag: true, isMarker: true}, len(ItemEnd)
	}

	if tagMatch := tagRegex.FindStringIndex(remaining); tagMatch != nil && tagMatch[0] == 0 {
		return htmlToken{val: remaining[:tagMatch[1]], isTag: true}, tagMatch[1]
	}

	nextBoundary := findNextBoundary(remaining)
	if nextBoundary > 0 {
		return htmlToken{val: remaining[:nextBoundary], isTag: false}, nextBoundary
	}

	return htmlToken{val: remaining, isTag: false}, len(remaining)
}

func findNextBoundary(remaining string) int {
	nextTag := len(remaining)

	if tagMatch := tagRegex.FindStringIndex(remaining); tagMatch != nil {
		nextTag = tagMatch[0]
	}

	if idx := strings.Index(remaining, ItemStart); idx >= 0 && idx < nextTag {
		nextTag = idx
	}

	if idx := strings.Index(remaining, ItemEnd); idx >= 0 && idx < nextTag {
		nextTag = idx
	}

	return nextTag
}

func calculateTotalTextLen(tokens []htmlToken) int {
	totalLen := 0

	for _, t := range tokens {
		if !t.isTag {
			totalLen += utf16Len(t.val)
		}
	}

	return totalLen
}

func (s *htmlSplitter) processTokens(tokens []htmlToken) {
	for i, t := range tokens {
		if t.isTag {
			s.processTagToken(t, tokens, i)
		} else {
			s.processTextToken(t.val)
		}
	}

	s.flush()
}

func (s *htmlSplitter) processTagToken(t htmlToken, tokens []htmlToken, idx int) {
	if !t.isMarker {
		if s.shouldSkipClosingTag(t.val) {
			return
		}

		s.openTags = updateOpenTags(t.val, s.openTags)
	}

	s.current.WriteString(t.val)

	if t.val == ItemEnd {
		s.maybeFlushAtItemEnd(tokens, idx)
	}
}

func (s *htmlSplitter) shouldSkipClosingTag(tagVal string) bool {
	matches := tagRegex.FindStringSubmatch(tagVal)
	if len(matches) < 3 || matches[1] != "/" {
		return false
	}

	tagName := strings.ToLower(matches[2])
	if !noReopenTags[tagName] {
		return false
	}

	for _, ot := range s.openTags {
		if strings.ToLower(GetTagName(ot)) == tagName {
			return false
		}
	}

	return true
}

func (s *htmlSplitter) maybeFlushAtItemEnd(tokens []htmlToken, idx int) {
	if s.currentLen <= s.limit/2 || idx+1 >= len(tokens) {
		return
	}

	nextToken := tokens[idx+1]
	if nextToken.isTag || !strings.HasPrefix(nextToken.val, "\n") {
		return
	}

	s.current.WriteString("\n")
	s.flush()

	tokens[idx+1] = htmlToken{val: strings.TrimPrefix(nextToken.val, "\n"), isTag: false}
}

func (s *htmlSplitter) processTextToken(text string) {
	remaining := text

	for len(remaining) > 0 {
		canTake := s.limit - s.currentLen
		if canTake <= 0 {
			s.flush()
			canTake = s.limit
		}

		remainingLen := utf16Len(remaining)
		if remainingLen <= canTake {
			s.current.WriteString(remaining)
			s.currentLen += remainingLen

			return
		}

		toWrite, newRemaining := findBestSplit(remaining, canTake)
		if len(toWrite) > 0 {
			s.current.WriteString(toWrite)
			s.currentLen += utf16Len(toWrite)
			remaining = strings.TrimLeft(newRemaining, " \t\n\r")
		}

		if len(remaining) > 0 {
			s.flush()
		}
	}
}

func (s *htmlSplitter) flush() {
	if s.current.Len() == 0 {
		return
	}

	tagsLen := 0
	for _, tag := range s.openTags {
		tagsLen += len(tag)
	}

	if s.current.Len() <= tagsLen {
		return
	}

	content := strings.TrimRight(s.current.String(), " \t")

	for i := len(s.openTags) - 1; i >= 0; i-- {
		content += "</" + GetTagName(s.openTags[i]) + ">"
	}

	s.parts = append(s.parts, content)
	s.current.Reset()
	s.currentLen = 0

	s.reopenTags()
}

func (s *htmlSplitter) reopenTags() {
	var newOpenTags []string

	for _, tag := range s.openTags {
		tagName := strings.ToLower(GetTagName(tag))
		if !noReopenTags[tagName] {
			newOpenTags = append(newOpenTags, tag)
			s.current.WriteString(tag)
		}
	}

	s.openTags = newOpenTags
}

func findBestSplit(text string, maxUnits int) (toWrite, remainder string) {
	textLen := utf16Len(text)
	if textLen <= maxUnits {
		return text, ""
	}

	searchText := utf16Slice(text, maxUnits)

	if toWrite, remainder := trySplitAfter(searchText, text); toWrite != "" {
		return toWrite, remainder
	}

	if toWrite, remainder := trySplitBefore(searchText, text); toWrite != "" {
		return toWrite, remainder
	}

	if pos := strings.LastIndex(searchText, "\n"); pos > 0 {
		return searchText[:pos+1], text[pos+1:]
	}

	if pos := strings.LastIndex(searchText, " "); pos > 0 {
		return searchText[:pos+1], text[pos+1:]
	}

	return searchText, text[len(searchText):]
}

func trySplitAfter(searchText, fullText string) (string, string) {
	for _, sep := range splitAfter {
		if pos := strings.LastIndex(searchText, sep); pos > 0 {
			splitAt := pos + len(sep)
			return searchText[:splitAt], fullText[splitAt:]
		}
	}

	return "", ""
}

func trySplitBefore(searchText, fullText string) (string, string) {
	for _, sep := range splitBefore {
		if pos := strings.LastIndex(searchText, sep); pos > 0 {
			splitAt := pos + 1
			return searchText[:splitAt], fullText[splitAt:]
		}
	}

	return "", ""
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
