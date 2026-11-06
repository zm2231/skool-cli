// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import "testing"

func TestSkoolDescToMarkdownBlocks(t *testing.T) {
	desc := `[v2][` +
		`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Plain and "},{"type":"text","text":"bold","marks":[{"type":"bold"}]},{"type":"text","text":" and "},{"type":"text","text":"it","marks":[{"type":"italic"}]}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"a link","marks":[{"type":"link","attrs":{"href":"https://x.co"}}]}]},` +
		`{"type":"image","attrs":{"alt":"pic","src":"https://img/x.png"}},` +
		`{"type":"unorderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},` +
		`{"type":"orderedList","attrs":{"start":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"third"}]}]}]},` +
		`{"type":"horizontalRule"}` +
		`]`
	got := skoolDescToMarkdown(desc)
	wants := []string{
		"## Title",
		"Plain and **bold** and *it*",
		"[a link](https://x.co)",
		"![pic](https://img/x.png)",
		"- one\n- two",
		"3. third",
		"---",
	}
	for _, w := range wants {
		if !contains(got, w) {
			t.Fatalf("expected %q in output, got:\n%s", w, got)
		}
	}
}

func TestSkoolDescToMarkdownLinkWrapsStyledRun(t *testing.T) {
	desc := `[v2][{"type":"paragraph","content":[{"type":"text","text":"click","marks":[{"type":"bold"},{"type":"link","attrs":{"href":"https://x.co"}}]}]}]`
	got := skoolDescToMarkdown(desc)
	if got != "[**click**](https://x.co)" {
		t.Fatalf("link must wrap the whole styled run, got %q", got)
	}
}

func TestSkoolDescToMarkdownNestedList(t *testing.T) {
	desc := `[v2][{"type":"unorderedList","content":[{"type":"listItem","content":[` +
		`{"type":"paragraph","content":[{"type":"text","text":"parent"}]},` +
		`{"type":"unorderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"child"}]}]}]}` +
		`]}]}]`
	got := skoolDescToMarkdown(desc)
	if !contains(got, "- parent") || !contains(got, "  - child") {
		t.Fatalf("nested list should indent the child, got:\n%s", got)
	}
}

func TestSkoolDescToMarkdownNonV2Passthrough(t *testing.T) {
	if got := skoolDescToMarkdown("just plain legacy text"); got != "just plain legacy text" {
		t.Fatalf("non-[v2] body must pass through verbatim, got %q", got)
	}
	if got := skoolDescToMarkdown("   "); got != "" {
		t.Fatalf("blank body must render empty, got %q", got)
	}
	if got := skoolDescToMarkdown("[v2]not json"); got != "[v2]not json" {
		t.Fatalf("unparseable [v2] must fall back verbatim, got %q", got)
	}
}

func TestSkoolDescToMarkdownHardBreak(t *testing.T) {
	desc := `[v2][{"type":"paragraph","content":[{"type":"text","text":"line one"},{"type":"hardBreak"},{"type":"text","text":"line two"}]}]`
	got := skoolDescToMarkdown(desc)
	if got != "line one\\\nline two" {
		t.Fatalf("hardBreak must render a Markdown hard break, got %q", got)
	}
}

func TestRedirectMD(t *testing.T) {
	cases := map[string]string{
		"/aistrategyexchange/classroom/a1e0c65f?md=4b3c0e38278a447eb0be2e833b76ef07": "4b3c0e38278a447eb0be2e833b76ef07",
		"/x/classroom/y?md=abc&foo=bar":                                              "abc",
		"/x/classroom/y?md=abc#frag":                                                 "abc",
		"/x/classroom/y":                                                             "",
		"/x/classroom/y?note=md=wrong&md=right":                                      "right",
		"/x/classroom/y?foo=bar&md=late":                                             "late",
	}
	for target, want := range cases {
		if got := redirectMD(target); got != want {
			t.Fatalf("redirectMD(%q)=%q, want %q", target, got, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
