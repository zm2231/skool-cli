// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

type richTextNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []richTextNode `json:"content"`
	Text    string         `json:"text"`
	Marks   []richTextMark `json:"marks"`
}

type richTextMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// skoolDescToMarkdown converts a Skool lesson `desc` to Markdown. A body ships
// as the literal `[v2]` followed by a ProseMirror block array. A non-[v2] or
// unparseable value is returned verbatim so a body is never dropped for having
// an unfamiliar shape.
func skoolDescToMarkdown(desc string) string {
	trimmed := strings.TrimSpace(desc)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "[v2]") {
		return trimmed
	}
	var nodes []richTextNode
	if err := json.Unmarshal([]byte(strings.TrimPrefix(trimmed, "[v2]")), &nodes); err != nil {
		return trimmed
	}
	return strings.TrimSpace(renderBlocks(nodes))
}

func renderBlocks(nodes []richTextNode) string {
	blocks := make([]string, 0, len(nodes))
	for _, n := range nodes {
		s := renderBlock(n)
		if strings.TrimSpace(s) != "" || n.Type == "horizontalRule" {
			blocks = append(blocks, s)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func renderBlock(n richTextNode) string {
	switch n.Type {
	case "paragraph":
		return renderInline(n.Content)
	case "heading":
		level := 2
		if v, ok := n.Attrs["level"].(float64); ok && v >= 1 && v <= 6 {
			level = int(v)
		}
		return strings.Repeat("#", level) + " " + renderInline(n.Content)
	case "image":
		alt, _ := n.Attrs["alt"].(string)
		src, _ := n.Attrs["src"].(string)
		if src == "" {
			src, _ = n.Attrs["originalSrc"].(string)
		}
		return fmt.Sprintf("![%s](%s)", alt, src)
	case "horizontalRule":
		return "---"
	case "blockquote":
		lines := strings.Split(renderBlocks(n.Content), "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight("> "+l, " ")
		}
		return strings.Join(lines, "\n")
	case "codeBlock":
		lang, _ := n.Attrs["language"].(string)
		return "```" + lang + "\n" + collectText(n.Content) + "\n```"
	case "unorderedList":
		return renderList(n, false, 0)
	case "orderedList":
		return renderList(n, true, 0)
	default:
		if len(n.Content) > 0 {
			return renderInline(n.Content)
		}
		return n.Text
	}
}

func renderList(list richTextNode, ordered bool, depth int) string {
	indent := strings.Repeat("  ", depth)
	index := 1
	if ordered {
		if v, ok := list.Attrs["start"].(float64); ok && v >= 1 {
			index = int(v)
		}
	}
	var lines []string
	for _, item := range list.Content {
		if item.Type != "listItem" {
			continue
		}
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", index)
			index++
		}
		var itemText string
		var nested []string
		for _, child := range item.Content {
			switch child.Type {
			case "unorderedList":
				nested = append(nested, renderList(child, false, depth+1))
			case "orderedList":
				nested = append(nested, renderList(child, true, depth+1))
			case "paragraph":
				// A listItem's first paragraph is the bullet text; any further
				// paragraph is a continuation line indented under the bullet.
				if itemText == "" {
					itemText = renderInline(child.Content)
				} else {
					nested = append(nested, indent+"  "+renderInline(child.Content))
				}
			default:
				if itemText == "" {
					itemText = renderInline(child.Content)
				}
			}
		}
		lines = append(lines, indent+marker+itemText)
		lines = append(lines, nested...)
	}
	return strings.Join(lines, "\n")
}

func renderInline(nodes []richTextNode) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(applyMarks(n.Text, n.Marks))
		case "hardBreak":
			b.WriteString("\\\n")
		default:
			if len(n.Content) > 0 {
				b.WriteString(renderInline(n.Content))
			} else {
				b.WriteString(n.Text)
			}
		}
	}
	return b.String()
}

func applyMarks(text string, marks []richTextMark) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	var href string
	var code, bold, italic bool
	for _, m := range marks {
		switch m.Type {
		case "bold", "strong":
			bold = true
		case "italic", "em":
			italic = true
		case "code":
			code = true
		case "link":
			if h, ok := m.Attrs["href"].(string); ok {
				href = h
			}
		}
	}
	// code innermost, link outermost so the whole styled run is the link label.
	if code {
		text = "`" + text + "`"
	}
	if bold {
		text = "**" + text + "**"
	}
	if italic {
		text = "*" + text + "*"
	}
	if href != "" {
		text = "[" + text + "](" + href + ")"
	}
	return text
}

func collectText(nodes []richTextNode) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Type == "text" {
			b.WriteString(n.Text)
		}
		if len(n.Content) > 0 {
			b.WriteString(collectText(n.Content))
		}
	}
	return b.String()
}
