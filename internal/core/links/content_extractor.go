package links

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/araddon/dateparse"
	"github.com/go-shiori/go-readability"
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

	// Extract using readability (Firefox Reader Mode algorithm)
	article, err := readability.FromReader(bytes.NewReader(htmlBytes), u)
	if err != nil {
		// Fall back to meta tags only - readability failure is not fatal
		meta := extractMetaTags(htmlBytes)

		//nolint:nilerr // fallback to meta tags when readability fails
		return &WebContent{
			Title:       meta.Title,
			Description: meta.Description,
		}, nil
	}

	meta := extractMetaTags(htmlBytes)
	jsonLD := extractJSONLD(htmlBytes)

	return &WebContent{
		Title:       coalesce(jsonLD.Title, article.Title, meta.OGTitle, meta.Title),
		Description: coalesce(jsonLD.Description, meta.OGDescription, meta.Description),
		Content:     truncate(article.TextContent, maxLen),
		Author:      coalesce(jsonLD.Author, article.Byline, meta.Author),
		PublishedAt: coalesceTime(parseDate(jsonLD.PublishedAt), parseDate(meta.PublishedTime)),
		ImageURL:    coalesce(jsonLD.Image, meta.OGImage),
		WordCount:   countWords(article.TextContent),
	}, nil
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
