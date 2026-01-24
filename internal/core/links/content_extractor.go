package links

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/readeck/go-readability/v2"
	"github.com/araddon/dateparse"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

type WebContent struct {
	Title       string
	Description string
	Content     string
	Author      string
	PublishedAt time.Time
	ImageURL    string
	WordCount   int
	Language    string
}

func ExtractWebContent(htmlBytes []byte, rawURL string, maxLen int) (*WebContent, error) {
	u, _ := url.Parse(rawURL) //nolint:errcheck // URL was already validated

	// Try RSS/Atom parsing first
	if feedContent, ok := tryExtractFeed(htmlBytes, maxLen); ok {
		return feedContent, nil
	}

	// Extract using readability (Firefox Reader Mode algorithm)
	article, err := readability.FromReader(bytes.NewReader(htmlBytes), u)
	if err != nil {
		// Fall back to meta tags only - readability failure is not fatal
		meta := extractMetaTags(htmlBytes)
		lang := DetectLanguage(meta.Title + " " + meta.Description)

		//nolint:nilerr // fallback to meta tags when readability fails
		return &WebContent{
			Title:       meta.Title,
			Description: meta.Description,
			Language:    lang,
		}, nil
	}

	meta := extractMetaTags(htmlBytes)
	jsonLD := extractJSONLD(htmlBytes)

	// Extract text content using v2 API
	textContent := extractArticleText(article)

	lang := DetectLanguage(textContent)
	if lang == "" {
		lang = DetectLanguage(meta.Title + " " + meta.Description)
	}

	return &WebContent{
		Title:       coalesce(jsonLD.Title, article.Title(), meta.OGTitle, meta.Title),
		Description: coalesce(jsonLD.Description, meta.OGDescription, meta.Description),
		Content:     truncate(textContent, maxLen),
		Author:      coalesce(jsonLD.Author, article.Byline(), meta.Author),
		PublishedAt: coalesceTime(parseDate(jsonLD.PublishedAt), parseDate(meta.PublishedTime)),
		ImageURL:    coalesce(jsonLD.Image, meta.OGImage),
		WordCount:   countWords(textContent),
		Language:    lang,
	}, nil
}

// extractArticleText extracts text content from a readability Article using v2 API.
func extractArticleText(article readability.Article) string {
	var buf bytes.Buffer
	if err := article.RenderText(&buf); err != nil {
		return ""
	}

	return buf.String()
}

func tryExtractFeed(htmlBytes []byte, maxLen int) (*WebContent, bool) {
	fp := gofeed.NewParser()

	feed, err := fp.Parse(bytes.NewReader(htmlBytes))
	if err != nil || len(feed.Items) == 0 {
		return nil, false
	}

	// If it's a feed, we take the first item as the content for the specific URL if it matches,
	// or just the first item if we are treating the feed URL as the source.
	item := feed.Items[0]
	lang := DetectLanguage(item.Title + " " + item.Description + " " + item.Content)

	return &WebContent{
		Title:       item.Title,
		Description: item.Description,
		Content:     truncate(htmlutils.StripHTMLTags(item.Content), maxLen),
		Author:      extractFeedAuthor(item),
		PublishedAt: coalesceTime(toTime(item.PublishedParsed), toTime(item.UpdatedParsed)),
		ImageURL:    extractFeedImage(item),
		WordCount:   countWords(item.Content),
		Language:    lang,
	}, true
}

func extractFeedAuthor(item *gofeed.Item) string {
	if item.Author != nil {
		return item.Author.Name
	}

	if len(item.Authors) > 0 {
		return item.Authors[0].Name
	}

	return ""
}

func extractFeedImage(item *gofeed.Item) string {
	if item.Image != nil {
		return item.Image.URL
	}

	for _, enclosure := range item.Enclosures {
		if strings.HasPrefix(enclosure.Type, "image/") {
			return enclosure.URL
		}
	}

	return ""
}

func toTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
}

type JSONLD struct {
	Title       string
	Description string
	Author      string
	PublishedAt string
	Image       string
}

func extractJSONLD(htmlBytes []byte) JSONLD {
	var ld JSONLD

	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return ld
	}

	var traverse func(*html.Node)

	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			for _, attr := range n.Attr {
				if attr.Key == "type" && attr.Val == "application/ld+json" {
					if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
						parseLDJSON(n.FirstChild.Data, &ld)
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)

	return ld
}

func parseLDJSON(data string, ld *JSONLD) {
	var v interface{}
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return
	}

	processLDValue(v, ld)
}

func processLDValue(v interface{}, ld *JSONLD) {
	switch m := v.(type) {
	case map[string]interface{}:
		extractFromLDMap(m, ld)

		// Also check @graph for nested structures
		if graph, ok := m["@graph"].([]interface{}); ok {
			for _, item := range graph {
				processLDValue(item, ld)
			}
		}
	case []interface{}:
		for _, item := range m {
			processLDValue(item, ld)
		}
	}
}

func extractFromLDMap(m map[string]interface{}, ld *JSONLD) {
	t, ok := m["@type"].(string)
	if !ok {
		return
	}

	if t != "NewsArticle" && t != "Article" && t != "BlogPosting" {
		return
	}

	if title, ok := m["headline"].(string); ok {
		ld.Title = title
	}

	if desc, ok := m["description"].(string); ok {
		ld.Description = desc
	}

	if date, ok := m["datePublished"].(string); ok {
		ld.PublishedAt = date
	}

	if author, ok := m["author"]; ok {
		ld.Author = extractLDAuthor(author)
	}

	if image, ok := m["image"]; ok {
		ld.Image = extractLDImage(image)
	}
}

func extractLDAuthor(v interface{}) string {
	switch a := v.(type) {
	case string:
		return a
	case map[string]interface{}:
		if name, ok := a["name"].(string); ok {
			return name
		}
	case []interface{}:
		if len(a) > 0 {
			return extractLDAuthor(a[0])
		}
	}

	return ""
}

func extractLDImage(v interface{}) string {
	switch img := v.(type) {
	case string:
		return img
	case map[string]interface{}:
		if url, ok := img["url"].(string); ok {
			return url
		}
	case []interface{}:
		if len(img) > 0 {
			return extractLDImage(img[0])
		}
	}

	return ""
}

func coalesceTime(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}

	return time.Time{}
}

type MetaTags struct {
	Title         string
	Description   string
	OGTitle       string
	OGDescription string
	OGImage       string
	Author        string
	PublishedTime string
}

func extractMetaTags(htmlBytes []byte) MetaTags {
	var meta MetaTags

	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return meta
	}

	var traverse func(*html.Node)

	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			processMetaElement(n, &meta)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)

	return meta
}

func processMetaElement(n *html.Node, meta *MetaTags) {
	switch n.Data {
	case "title":
		if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			meta.Title = strings.TrimSpace(n.FirstChild.Data)
		}
	case "meta":
		applyMetaTag(n, meta)
	}
}

func applyMetaTag(n *html.Node, meta *MetaTags) {
	name, content := getMetaAttrs(n)

	switch strings.ToLower(name) {
	case "description":
		meta.Description = content
	case "author":
		meta.Author = content
	case "og:title":
		meta.OGTitle = content
	case "og:description":
		meta.OGDescription = content
	case "og:image":
		meta.OGImage = content
	case "article:published_time":
		meta.PublishedTime = content
	}
}

func getMetaAttrs(n *html.Node) (string, string) {
	var name, content string

	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "name", "property":
			name = attr.Val
		case "content":
			content = attr.Val
		}
	}

	return name, content
}

func coalesce(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}

	return ""
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}

	runes := []rune(s)

	return string(runes[:max]) + "..."
}

func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	t, err := dateparse.ParseAny(s)
	if err != nil {
		return time.Time{}
	}

	return t
}

func countWords(s string) int {
	return len(strings.Fields(s))
}
