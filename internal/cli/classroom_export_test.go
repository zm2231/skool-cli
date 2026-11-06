// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGetter struct {
	responses map[string]json.RawMessage
	failOn    map[string]bool
	calls     []string
}

func (f *fakeGetter) Get(_ string, params map[string]string) (json.RawMessage, error) {
	md := params["md"]
	f.calls = append(f.calls, md)
	if f.failOn[md] {
		return nil, fmt.Errorf("simulated transport failure for md=%s", md)
	}
	r, ok := f.responses[md]
	if !ok {
		return nil, fmt.Errorf("no fixture for md=%q", md)
	}
	return r, nil
}

type fixLesson struct {
	id, title, desc string
}

// courseTreeJSON renders the pageProps.course envelope Skool returns for one
// selected md: every lesson appears in the tree but only `selected` carries a
// desc body, mirroring the live behavior the export must work around.
func courseTreeJSON(t *testing.T, lessons []fixLesson, selected string) json.RawMessage {
	t.Helper()
	children := make([]map[string]any, 0, len(lessons))
	for _, l := range lessons {
		meta := map[string]any{"title": l.title}
		if l.id == selected {
			meta["desc"] = l.desc
		}
		children = append(children, map[string]any{
			"course": map[string]any{"id": l.id, "unitType": "module", "metadata": meta},
		})
	}
	env := map[string]any{"pageProps": map[string]any{"course": map[string]any{
		"course":   map[string]any{"id": "course1", "unitType": "course", "metadata": map[string]any{"title": "Test Course"}},
		"children": children,
	}}}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureGetter(t *testing.T, lessons []fixLesson) *fakeGetter {
	t.Helper()
	first := lessons[0].id
	responses := map[string]json.RawMessage{
		"": json.RawMessage(fmt.Sprintf(`{"pageProps":{"__N_REDIRECT":"/comm/classroom/slug?md=%s"}}`, first)),
	}
	for _, l := range lessons {
		responses[l.id] = courseTreeJSON(t, lessons, l.id)
	}
	return &fakeGetter{responses: responses, failOn: map[string]bool{}}
}

func TestRunClassroomExportWritesBodies(t *testing.T) {
	lessons := []fixLesson{
		{"L1", "Welcome", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"hello world\"}]}]"},
		{"L2", "Next Steps", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"do the thing\"}]}]"},
	}
	g := fixtureGetter(t, lessons)
	out := filepath.Join(t.TempDir(), "course")
	res, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if res.LessonCount != 2 || res.BodiesFound != 2 {
		t.Fatalf("want 2 lessons/2 bodies, got %d/%d", res.LessonCount, res.BodiesFound)
	}
	welcome, err := os.ReadFile(filepath.Join(out, "welcome.md"))
	if err != nil {
		t.Fatalf("welcome.md not written: %v", err)
	}
	if !contains(string(welcome), "hello world") || !contains(string(welcome), "lesson_id: L1") {
		t.Fatalf("welcome.md missing body or frontmatter:\n%s", welcome)
	}
	for _, l := range lessons {
		found := false
		for _, c := range g.calls {
			if c == l.id {
				found = true
			}
		}
		if !found {
			t.Fatalf("lesson %s was never fetched by its md", l.id)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "index.md")); err != nil {
		t.Fatalf("index.md not written: %v", err)
	}
}

func TestLessonMarkdownMuxAndAttachments(t *testing.T) {
	meta := map[string]any{
		"videoId":       "vid123",
		"videoLinkData": map[string]any{"mux": "https://stream.mux.com/abc.m3u8"},
		"attachments":   `[{"title":"Slides","file_id":"f1"}]`,
		"resources":     `[{"title":"Prompt","file_id":"r1"}]`,
	}
	got := lessonMarkdown("Lesson One", "L1", meta, "body text")
	for _, want := range []string{
		"mux_url: \"https://stream.mux.com/abc.m3u8\"",
		`attachments: [{"title":"Slides","file_id":"f1"}]`,
		"video_id: \"vid123\"",
		`resources: [{"title":"Prompt","file_id":"r1"}]`,
	} {
		if !contains(got, want) {
			t.Fatalf("frontmatter missing %q:\n%s", want, got)
		}
	}
	bare := lessonMarkdown("L", "L", map[string]any{}, "b")
	for _, absent := range []string{"mux_url:", "attachments:"} {
		if contains(bare, absent) {
			t.Fatalf("%s must not emit when its metadata is absent:\n%s", absent, bare)
		}
	}
	empty := lessonMarkdown("L", "L", map[string]any{"attachments": "[]", "resources": "[]"}, "b")
	if contains(empty, "attachments:") || contains(empty, "resources:") {
		t.Fatalf("empty-array metadata must suppress the frontmatter line:\n%s", empty)
	}
}

func TestRunClassroomExportTitleCollision(t *testing.T) {
	lessons := []fixLesson{
		{"L1", "Introduction", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"first intro\"}]}]"},
		{"L2", "Introduction", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"second intro\"}]}]"},
	}
	g := fixtureGetter(t, lessons)
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	mdCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" && e.Name() != "index.md" {
			mdCount++
		}
	}
	if mdCount != 2 {
		t.Fatalf("colliding titles must yield 2 distinct files, got %d: %v", mdCount, entries)
	}
	first, _ := os.ReadFile(filepath.Join(out, "introduction.md"))
	second, _ := os.ReadFile(filepath.Join(out, "introduction-L2.md"))
	if !contains(string(first), "first intro") || !contains(string(second), "second intro") {
		t.Fatalf("collision files lost distinct bodies:\n1:%s\n2:%s", first, second)
	}
}

func TestRunClassroomExportFetchErrorAborts(t *testing.T) {
	lessons := []fixLesson{
		{"L1", "One", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"body one\"}]}]"},
		{"L2", "Two", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"body two\"}]}]"},
	}
	g := fixtureGetter(t, lessons)
	g.failOn["L2"] = true
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err == nil {
		t.Fatal("a per-lesson fetch failure must abort the export")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("failed export must leave no consumable out dir, but %s exists", out)
	}
	parent := filepath.Dir(out)
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Fatalf("staging dir was not cleaned up: %s", e.Name())
		}
	}
}

func TestRunClassroomExportZeroBodiesGuard(t *testing.T) {
	lessons := []fixLesson{{"L1", "Video Only", ""}, {"L2", "Also Video", ""}}
	g := fixtureGetter(t, lessons)
	out := filepath.Join(t.TempDir(), "course")

	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err == nil {
		t.Fatal("zero bodies without --allow-empty must error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("zero-body failure must leave no out dir, but %s exists", out)
	}

	g2 := fixtureGetter(t, lessons)
	if _, err := runClassroomExport(g2, "comm", "slug", out, true, io.Discard); err != nil {
		t.Fatalf("--allow-empty must permit a titles-only export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "video-only.md")); err != nil {
		t.Fatalf("--allow-empty should still write titles-only files: %v", err)
	}
}

func TestRunClassroomExportSuffixCollision(t *testing.T) {
	// Three same-title lessons whose ids share an 8-char prefix: neither the
	// title slug nor the id-suffix disambiguates, so the allocator must fall
	// through to a numeric suffix and still produce three distinct files.
	body := func(s string) string {
		return `[v2][{"type":"paragraph","content":[{"type":"text","text":"` + s + `"}]}]`
	}
	lessons := []fixLesson{
		{"abcd1234aaaa", "Intro", body("first")},
		{"abcd1234bbbb", "Intro", body("second")},
		{"abcd1234cccc", "Intro", body("third")},
	}
	g := fixtureGetter(t, lessons)
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, want := range []string{"first", "second", "third"} {
		matched := ""
		entries, _ := os.ReadDir(out)
		for _, e := range entries {
			if seen[e.Name()] || e.Name() == "index.md" {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(out, e.Name()))
			if contains(string(data), want) {
				matched = e.Name()
				break
			}
		}
		if matched == "" {
			t.Fatalf("body %q not found in any distinct file", want)
		}
		seen[matched] = true
	}
	if len(seen) != 3 {
		t.Fatalf("want 3 distinct files, got %d", len(seen))
	}
}

func TestRunClassroomExportMissingSelectedNode(t *testing.T) {
	lessons := []fixLesson{
		{"L1", "One", `[v2][{"type":"paragraph","content":[{"type":"text","text":"body one"}]}]`},
		{"L2", "Two", `[v2][{"type":"paragraph","content":[{"type":"text","text":"body two"}]}]`},
	}
	g := fixtureGetter(t, lessons)
	// L2's response is valid JSON but omits the L2 node entirely.
	g.responses["L2"] = courseTreeJSON(t, lessons[:1], "L1")
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err == nil {
		t.Fatal("a selected lesson missing from its own response must abort")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("missing-node failure must leave no out dir, but %s exists", out)
	}
}

func TestRunClassroomExportSectionSlugCollision(t *testing.T) {
	lessons := []fixLesson{
		{"L1", "Plus Lesson", `[v2][{"type":"paragraph","content":[{"type":"text","text":"cpp body"}]}]`},
		{"L2", "Sharp Lesson", `[v2][{"type":"paragraph","content":[{"type":"text","text":"csharp body"}]}]`},
	}
	build := func(selected string) json.RawMessage {
		section := func(title, lid, ltitle, desc string) map[string]any {
			meta := map[string]any{"title": ltitle}
			if lid == selected {
				meta["desc"] = desc
			}
			return map[string]any{
				"course":   map[string]any{"id": "sec-" + title, "unitType": "module", "metadata": map[string]any{"title": title}},
				"children": []map[string]any{{"course": map[string]any{"id": lid, "unitType": "module", "metadata": meta}}},
			}
		}
		env := map[string]any{"pageProps": map[string]any{"course": map[string]any{
			"course": map[string]any{"id": "course1", "unitType": "course", "metadata": map[string]any{"title": "C"}},
			"children": []map[string]any{
				section("C++", "L1", "Plus Lesson", `[v2][{"type":"paragraph","content":[{"type":"text","text":"cpp body"}]}]`),
				section("C#", "L2", "Sharp Lesson", `[v2][{"type":"paragraph","content":[{"type":"text","text":"csharp body"}]}]`),
			},
		}}}
		b, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	g := &fakeGetter{failOn: map[string]bool{}, responses: map[string]json.RawMessage{
		"":   json.RawMessage(`{"pageProps":{"__N_REDIRECT":"/comm/classroom/slug?md=L1"}}`),
		"L1": build("L1"),
		"L2": build("L2"),
	}}
	_ = lessons
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	dirs := 0
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 2 {
		t.Fatalf("sections C++ and C# must map to 2 distinct dirs, got %d: %v", dirs, entries)
	}
	var cpp, csharp bool
	filepath.Walk(out, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			data, _ := os.ReadFile(p)
			if contains(string(data), "cpp body") {
				cpp = true
			}
			if contains(string(data), "csharp body") {
				csharp = true
			}
		}
		return nil
	})
	if !cpp || !csharp {
		t.Fatalf("both section bodies must survive (cpp=%v csharp=%v)", cpp, csharp)
	}
}

func TestRunClassroomExportSameTitleSiblingSections(t *testing.T) {
	build := func(selected string) json.RawMessage {
		section := func(secID, lid, ltitle, desc string) map[string]any {
			meta := map[string]any{"title": ltitle}
			if lid == selected {
				meta["desc"] = desc
			}
			return map[string]any{
				"course":   map[string]any{"id": secID, "unitType": "module", "metadata": map[string]any{"title": "Module"}},
				"children": []map[string]any{{"course": map[string]any{"id": lid, "unitType": "module", "metadata": meta}}},
			}
		}
		env := map[string]any{"pageProps": map[string]any{"course": map[string]any{
			"course": map[string]any{"id": "course1", "unitType": "course", "metadata": map[string]any{"title": "C"}},
			"children": []map[string]any{
				// Both sections share the SAME server id and title: identity must
				// come from tree position, not the (duplicate) id.
				section("dup", "L1", "Alpha", `[v2][{"type":"paragraph","content":[{"type":"text","text":"alpha body"}]}]`),
				section("dup", "L2", "Beta", `[v2][{"type":"paragraph","content":[{"type":"text","text":"beta body"}]}]`),
			},
		}}}
		b, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	g := &fakeGetter{failOn: map[string]bool{}, responses: map[string]json.RawMessage{
		"":   json.RawMessage(`{"pageProps":{"__N_REDIRECT":"/comm/classroom/slug?md=L1"}}`),
		"L1": build("L1"),
		"L2": build("L2"),
	}}
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	dirs := 0
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 2 {
		t.Fatalf("two sibling sections both titled 'Module' must map to 2 dirs, got %d: %v", dirs, entries)
	}
	var alpha, beta bool
	filepath.Walk(out, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			data, _ := os.ReadFile(p)
			if contains(string(data), "alpha body") {
				alpha = true
			}
			if contains(string(data), "beta body") {
				beta = true
			}
		}
		return nil
	})
	if !alpha || !beta {
		t.Fatalf("both sibling-section lessons must survive in distinct dirs (alpha=%v beta=%v)", alpha, beta)
	}
	index, _ := os.ReadFile(filepath.Join(out, "index.md"))
	headings := 0
	for _, line := range strings.Split(string(index), "\n") {
		if line == "## Module" {
			headings++
		}
	}
	if headings != 2 {
		t.Fatalf("index.md must keep two distinct '## Module' groups, got %d:\n%s", headings, index)
	}
}

func TestRunClassroomExportMixedDepthIndexOneGroupPerSection(t *testing.T) {
	// A module with a direct lesson, a nested subsection, then another direct
	// lesson. DFS visits A, B (under sub), C; the index must still show one
	// "## Module" and one "### Sub", not a repeated "## Module".
	p := func(text string) string {
		return `[v2][{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}]`
	}
	build := func(selected string) json.RawMessage {
		leaf := func(id, title, text string) map[string]any {
			meta := map[string]any{"title": title}
			if id == selected {
				meta["desc"] = p(text)
			}
			return map[string]any{"course": map[string]any{"id": id, "unitType": "module", "metadata": meta}}
		}
		module := map[string]any{
			"course": map[string]any{"id": "mod", "unitType": "module", "metadata": map[string]any{"title": "Module"}},
			"children": []map[string]any{
				leaf("A", "Lesson A", "body a"),
				{
					"course":   map[string]any{"id": "sub", "unitType": "module", "metadata": map[string]any{"title": "Sub"}},
					"children": []map[string]any{leaf("B", "Lesson B", "body b")},
				},
				leaf("C", "Lesson C", "body c"),
			},
		}
		env := map[string]any{"pageProps": map[string]any{"course": map[string]any{
			"course":   map[string]any{"id": "course1", "unitType": "course", "metadata": map[string]any{"title": "C"}},
			"children": []map[string]any{module},
		}}}
		b, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	g := &fakeGetter{failOn: map[string]bool{}, responses: map[string]json.RawMessage{
		"":  json.RawMessage(`{"pageProps":{"__N_REDIRECT":"/comm/classroom/slug?md=A"}}`),
		"A": build("A"), "B": build("B"), "C": build("C"),
	}}
	out := filepath.Join(t.TempDir(), "course")
	if _, err := runClassroomExport(g, "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(out, "index.md"))
	lines := strings.Split(string(index), "\n")
	mod, sub := 0, 0
	var subLine, lessonCLine = -1, -1
	for i, line := range lines {
		switch {
		case line == "## Module":
			mod++
		case line == "### Sub":
			sub++
			subLine = i
		case strings.Contains(line, "Lesson C"):
			lessonCLine = i
		}
	}
	if mod != 1 || sub != 1 {
		t.Fatalf("mixed-depth section must emit one '## Module' and one '### Sub', got %d/%d:\n%s", mod, sub, index)
	}
	if lessonCLine < 0 || subLine < 0 || lessonCLine > subLine {
		t.Fatalf("Lesson C (direct under Module) must be listed before '### Sub', not nested under it:\n%s", index)
	}
}

func TestPromoteAtomicRestoreFailureNamesBackup(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "course")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(out, "good.md"), []byte("prior export"), 0o644)
	staging, _ := os.MkdirTemp(base, ".staging-*")
	os.WriteFile(filepath.Join(staging, "new.md"), []byte("new export"), 0o644)

	// Fail both the promotion (src==staging) and the restore (src==backup path).
	rename := func(src, dst string) error {
		if src == staging || filepath.Base(src) == "prev" {
			return fmt.Errorf("simulated failure renaming %s", filepath.Base(src))
		}
		return os.Rename(src, dst)
	}
	err := promoteAtomic(staging, out, rename)
	if err == nil {
		t.Fatal("double rename failure must return an error")
	}
	if !contains(err.Error(), "preserved at") || !contains(err.Error(), ".classroom-backup") {
		t.Fatalf("error must name the retained backup path, got: %v", err)
	}
	found := false
	filepath.Walk(base, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && filepath.Base(p) == "good.md" {
			data, _ := os.ReadFile(p)
			if string(data) == "prior export" {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatal("the prior export must still exist on disk (at the backup path) after a failed restore")
	}
}

func TestPromoteAtomicRestoresOnFailure(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "course")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "good.md"), []byte("prior export"), 0o644); err != nil {
		t.Fatal(err)
	}
	staging, err := os.MkdirTemp(base, ".staging-*")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(staging, "new.md"), []byte("new export"), 0o644)

	failingRename := func(src, dst string) error {
		if src == staging {
			return fmt.Errorf("simulated promotion failure")
		}
		return os.Rename(src, dst)
	}
	if err := promoteAtomic(staging, out, failingRename); err == nil {
		t.Fatal("promoteAtomic must surface the rename failure")
	}
	data, err := os.ReadFile(filepath.Join(out, "good.md"))
	if err != nil || string(data) != "prior export" {
		t.Fatalf("failed promotion must restore the prior export intact, got %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(out, "new.md")); !os.IsNotExist(err) {
		t.Fatal("new content must not appear after a failed promotion")
	}
}

func TestRunClassroomExportAtomicReplace(t *testing.T) {
	lessons := []fixLesson{{"L1", "Only", "[v2][{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"real body\"}]}]"}}
	out := filepath.Join(t.TempDir(), "course")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(out, "stale.md")
	if err := os.WriteFile(sentinel, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runClassroomExport(fixtureGetter(t, lessons), "comm", "slug", out, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("a successful export must replace the prior out dir, not merge into it")
	}
	if _, err := os.Stat(filepath.Join(out, "only.md")); err != nil {
		t.Fatalf("new content missing after atomic replace: %v", err)
	}
}
