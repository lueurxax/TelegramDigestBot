package links

import (
	"bytes"
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

	return &WebContent{
		Title:       coalesce(article.Title, meta.OGTitle, meta.Title),
		Description: coalesce(meta.OGDescription, meta.Description),
		Content:     truncate(article.TextContent, maxLen),
		Author:      coalesce(article.Byline, meta.Author),
		PublishedAt: parseDate(meta.PublishedTime),
		ImageURL:    meta.OGImage,
		WordCount:   countWords(article.TextContent),
	}, nil
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
