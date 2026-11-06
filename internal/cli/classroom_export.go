// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type courseGetter interface {
	Get(path string, params map[string]string) (json.RawMessage, error)
}

type courseTreeNode struct {
	Course struct {
		ID       string         `json:"id"`
		Name     string         `json:"name"`
		UnitType string         `json:"unitType"`
		Metadata map[string]any `json:"metadata"`
	} `json:"course"`
	Children []courseTreeNode `json:"children"`
}

type sectionRef struct {
	ID    string
	Title string
}

type exportedLesson struct {
	ID        string
	Title     string
	Section   []sectionRef
	BodyChars int
	File      string
}

type exportResult struct {
	CourseSlug  string   `json:"course_slug"`
	CourseName  string   `json:"course_name"`
	OutDir      string   `json:"out_dir"`
	LessonCount int      `json:"lesson_count"`
	BodiesFound int      `json:"bodies_found"`
	FileCount   int      `json:"file_count"`
	Files       []string `json:"files"`
}

func newClassroomExportCmd(flags *rootFlags) *cobra.Command {
	var flagCommunity string
	var flagOut string
	var flagAllowEmpty bool

	cmd := &cobra.Command{
		Use:         "export <course-slug>",
		Short:       "Export a course to a folder of markdown files (one per lesson)",
		Example:     "  skool-pp-cli classroom export a1e0c65f --community aistrategyexchange --out ./course/",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if len(args) == 0 {
				return cmd.Help()
			}
			courseSlug := strings.TrimSpace(args[0])
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			community := strings.TrimSpace(flagCommunity)
			if community == "" && c.Config != nil {
				community = c.Config.TemplateVars["community"]
			}
			if community == "" {
				return usageErr(fmt.Errorf("--community is required"))
			}
			outDir := flagOut
			if outDir == "" {
				outDir = "./" + courseSlug + "/"
			}

			result, err := runClassroomExport(c, community, courseSlug, outDir, flagAllowEmpty, os.Stderr)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			if flags.asJSON {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
					"course_slug":  result.CourseSlug,
					"course_name":  result.CourseName,
					"out_dir":      result.OutDir,
					"lesson_count": result.LessonCount,
					"bodies_found": result.BodiesFound,
					"file_count":   result.FileCount,
					"files":        result.Files,
				}, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d files (%d/%d lessons with bodies) to %s\n",
				result.FileCount, result.BodiesFound, result.LessonCount, result.OutDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagCommunity, "community", "", "Community slug (defaults to template_vars.community)")
	cmd.Flags().StringVar(&flagOut, "out", "", "Output directory (default ./<course-slug>/)")
	cmd.Flags().BoolVar(&flagAllowEmpty, "allow-empty", false, "Write titles-only files even when no lesson bodies are extracted")
	return cmd
}

// runClassroomExport fetches a whole course and writes one markdown file per
// lesson. It builds the bundle in a sibling temp directory and atomically
// promotes it to outDir only on full success; any failure (a per-lesson fetch
// error, or zero bodies without allowEmpty) removes the staging directory so a
// downstream importer never sees a partial or empty export as a real course.
func runClassroomExport(g courseGetter, community, courseSlug, outDir string, allowEmpty bool, progress io.Writer) (*exportResult, error) {
	path := "/_next/data/{buildId}/{community}/classroom/{course_slug}.json"
	path = replacePathParam(path, "community", community)
	path = replacePathParam(path, "course_slug", courseSlug)

	fetch := func(md string) (courseTreeNode, string, error) {
		params := map[string]string{"g": community}
		if md != "" {
			params["md"] = md
		}
		raw, err := g.Get(path, params)
		if err != nil {
			return courseTreeNode{}, "", err
		}
		var env struct {
			PageProps struct {
				Course   courseTreeNode `json:"course"`
				Redirect string         `json:"__N_REDIRECT"`
			} `json:"pageProps"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return courseTreeNode{}, "", fmt.Errorf("parsing course response: %w", err)
		}
		return env.PageProps.Course, env.PageProps.Redirect, nil
	}

	// The bare course URL 307-redirects to ?md=<first-lesson-id>; only the
	// selected md carries a lesson body, so the tree is seeded from that
	// redirect and every lesson is then re-fetched by its own id.
	root, redirect, err := fetch("")
	if err != nil {
		return nil, err
	}
	if redirect != "" {
		root, _, err = fetch(redirectMD(redirect))
		if err != nil {
			return nil, err
		}
	}
	if root.Course.ID == "" {
		return nil, fmt.Errorf("no course found for slug %q in %s", courseSlug, community)
	}

	lessons := collectLessons(root, "r", nil)
	if len(lessons) == 0 {
		return nil, fmt.Errorf("course %q has no lessons", courseSlug)
	}

	parent := filepath.Dir(strings.TrimRight(outDir, "/"))
	if parent == "" {
		parent = "."
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("creating parent dir: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".classroom-export-*")
	if err != nil {
		return nil, fmt.Errorf("creating staging dir: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(staging)
		}
	}()

	courseTitle := metaTitle(root.Course.Metadata, root.Course.Name)
	dirs := newDirAllocator(staging)
	bodiesFound := 0
	var fetchErrs []string
	fileRel := map[string]string{}

	for i := range lessons {
		lesson := &lessons[i]
		node, _, ferr := fetch(lesson.ID)
		if ferr != nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", lesson.Title, ferr))
			continue
		}
		// A selected lesson absent from its own response is a retrieval failure,
		// not a video-only lesson; only a present node with no desc is video-only.
		found := findNode(node, lesson.ID)
		if found == nil {
			fetchErrs = append(fetchErrs, fmt.Sprintf("%s: lesson %s missing from its own response", lesson.Title, lesson.ID))
			continue
		}
		body := ""
		if desc, ok := found.Course.Metadata["desc"].(string); ok {
			body = skoolDescToMarkdown(desc)
		}
		if strings.TrimSpace(body) != "" {
			bodiesFound++
		}

		sectionDir := dirs.sectionPath(lesson.Section)
		if err := os.MkdirAll(sectionDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating section dir: %w", err)
		}
		filename := dirs.file(sectionDir, lesson.Title, lesson.ID)
		md := lessonMarkdown(lesson.Title, lesson.ID, found.Course.Metadata, body)
		if err := os.WriteFile(filename, []byte(md), 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", filename, err)
		}
		lesson.File = filename
		lesson.BodyChars = len(strings.TrimSpace(body))
		rel, _ := filepath.Rel(staging, filename)
		fileRel[lesson.ID] = rel
		if progress != nil {
			fmt.Fprintf(progress, "  [%d/%d] %s (%d chars)\n", i+1, len(lessons), lesson.Title, lesson.BodyChars)
		}
	}

	if len(fetchErrs) > 0 {
		return nil, fmt.Errorf("failed to fetch %d/%d lessons for %q; refusing to emit a partial export:\n  %s",
			len(fetchErrs), len(lessons), courseSlug, strings.Join(fetchErrs, "\n  "))
	}
	if bodiesFound == 0 && !allowEmpty {
		return nil, fmt.Errorf("exported %d lessons but extracted 0 bodies for %q; the course page shape may have changed. Re-run with --allow-empty to write titles only", len(lessons), courseSlug)
	}

	indexLines := append([]string{"# " + courseTitle, ""}, renderCourseIndex(root, 0, fileRel)...)
	if err := os.WriteFile(filepath.Join(staging, "index.md"), []byte(strings.Join(indexLines, "\n")+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("writing index: %w", err)
	}

	if err := promoteAtomic(staging, outDir, os.Rename); err != nil {
		return nil, err
	}
	committed = true

	files := make([]string, 0, len(lessons)+1)
	for i := range lessons {
		if lessons[i].File != "" {
			rel, _ := filepath.Rel(staging, lessons[i].File)
			files = append(files, filepath.Join(outDir, rel))
		}
	}
	files = append(files, filepath.Join(outDir, "index.md"))

	return &exportResult{
		CourseSlug:  courseSlug,
		CourseName:  courseTitle,
		OutDir:      outDir,
		LessonCount: len(lessons),
		BodiesFound: bodiesFound,
		FileCount:   len(files),
		Files:       files,
	}, nil
}

// dirAllocator hands out collision-free on-disk paths. Section titles and
// lesson titles that slugify to the same string (e.g. "C++" and "C#" -> "c",
// or two "Introduction" lessons) must not merge or overwrite, so every child
// name is deduped within its parent directory, and a section's resolved
// directory is cached by its ancestor identity path so all its lessons land
// together and distinct same-titled sections stay separate.
type dirAllocator struct {
	root       string
	sectionDir map[string]string
	children   map[string]map[string]bool
}

func newDirAllocator(root string) *dirAllocator {
	return &dirAllocator{
		root:       root,
		sectionDir: map[string]string{},
		children:   map[string]map[string]bool{},
	}
}

func (a *dirAllocator) used(dir string) map[string]bool {
	if a.children[dir] == nil {
		a.children[dir] = map[string]bool{}
	}
	return a.children[dir]
}

// sectionPath keys the cache by a length-prefixed ancestor-identity path (not
// titles) so two sibling sections that share a title still resolve to distinct
// directories, while the on-disk directory name is the deduped title slug.
func (a *dirAllocator) sectionPath(section []sectionRef) string {
	dir := a.root
	key := ""
	for _, s := range section {
		key += strconv.Itoa(len(s.ID)) + ":" + s.ID
		if d, ok := a.sectionDir[key]; ok {
			dir = d
			continue
		}
		dir = filepath.Join(dir, dedupeName(a.used(dir), slugify(s.Title), s.ID))
		a.sectionDir[key] = dir
	}
	return dir
}

func (a *dirAllocator) file(dir, title, id string) string {
	return filepath.Join(dir, dedupeName(a.used(dir), slugify(title), id)+".md")
}

func dedupeName(used map[string]bool, base, idHint string) string {
	if !used[base] {
		used[base] = true
		return base
	}
	if idHint != "" {
		h := idHint
		if len(h) > 8 {
			h = h[:8]
		}
		if c := base + "-" + h; !used[c] {
			used[c] = true
			return c
		}
	}
	for n := 2; ; n++ {
		if c := fmt.Sprintf("%s-%d", base, n); !used[c] {
			used[c] = true
			return c
		}
	}
}

// promoteAtomic replaces outDir with staging without a window in which neither
// exists: any prior export is moved into a freshly reserved backup directory
// first, and a failed promotion restores it. If the restore itself fails the
// error names the backup path so the prior export is never silently lost.
// rename is injectable so the failure paths are testable.
func promoteAtomic(staging, outDir string, rename func(string, string) error) error {
	var backupDir, backup string
	if _, err := os.Stat(outDir); err == nil {
		parent := filepath.Dir(strings.TrimRight(outDir, "/"))
		if parent == "" {
			parent = "."
		}
		d, err := os.MkdirTemp(parent, ".classroom-backup-*")
		if err != nil {
			return fmt.Errorf("staging backup dir: %w", err)
		}
		backupDir = d
		backup = filepath.Join(d, "prev")
		if err := rename(outDir, backup); err != nil {
			os.RemoveAll(backupDir)
			return fmt.Errorf("moving prior export aside: %w", err)
		}
	}
	if err := rename(staging, outDir); err != nil {
		if backup != "" {
			if rerr := rename(backup, outDir); rerr != nil {
				return fmt.Errorf("promoting export to %s failed (%v) and restoring the prior export failed (%v); the prior export is preserved at %s", outDir, err, rerr, backup)
			}
			os.RemoveAll(backupDir)
		}
		return fmt.Errorf("promoting export to %s: %w", outDir, err)
	}
	if backupDir != "" {
		os.RemoveAll(backupDir)
	}
	return nil
}

func redirectMD(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return u.Query().Get("md")
}

// collectLessons flattens the course tree to its leaf lessons. Each ancestor
// section is identified by its tree POSITION (posKey), not its server id, so
// distinct sections never share an allocator identity even when Skool hands out
// duplicate, empty, or separator-bearing ids.
func collectLessons(node courseTreeNode, posKey string, section []sectionRef) []exportedLesson {
	if len(node.Children) == 0 {
		return []exportedLesson{{
			ID:      node.Course.ID,
			Title:   metaTitle(node.Course.Metadata, node.Course.Name),
			Section: append([]sectionRef(nil), section...),
		}}
	}
	var out []exportedLesson
	for i, child := range node.Children {
		childSection := section
		if node.Course.UnitType != "course" {
			childSection = append(append([]sectionRef(nil), section...),
				sectionRef{ID: posKey, Title: metaTitle(node.Course.Metadata, node.Course.Name)})
		}
		out = append(out, collectLessons(child, posKey+"."+strconv.Itoa(i), childSection)...)
	}
	return out
}

// renderCourseIndex renders the index outline from the course tree rather than
// from lesson-emission order: each section heads its own group once (nested by
// depth), and a section's direct lessons are listed before its subsections so a
// lesson never falls under a sibling subsection's heading. depth is the node's
// tree depth; the course root (depth 0) prints no heading.
func renderCourseIndex(node courseTreeNode, depth int, fileRel map[string]string) []string {
	if len(node.Children) == 0 {
		if rel, ok := fileRel[node.Course.ID]; ok {
			return []string{"- [" + metaTitle(node.Course.Metadata, node.Course.Name) + "](" + rel + ")"}
		}
		return nil
	}
	var lines []string
	if node.Course.UnitType != "course" {
		lines = append(lines, "", strings.Repeat("#", depth+1)+" "+metaTitle(node.Course.Metadata, node.Course.Name))
	}
	for _, child := range node.Children {
		if len(child.Children) == 0 {
			lines = append(lines, renderCourseIndex(child, depth+1, fileRel)...)
		}
	}
	for _, child := range node.Children {
		if len(child.Children) > 0 {
			lines = append(lines, renderCourseIndex(child, depth+1, fileRel)...)
		}
	}
	return lines
}

func findNode(node courseTreeNode, id string) *courseTreeNode {
	if node.Course.ID == id {
		return &node
	}
	for i := range node.Children {
		if found := findNode(node.Children[i], id); found != nil {
			return found
		}
	}
	return nil
}

func metaTitle(metadata map[string]any, fallback string) string {
	if metadata != nil {
		if t, ok := metadata["title"].(string); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	if fallback != "" {
		return fallback
	}
	return "untitled"
}

func lessonMarkdown(title, id string, lessonMeta map[string]any, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "lesson_id: %s\n", id)
	fmt.Fprintf(&b, "title: %q\n", title)
	if lessonMeta != nil {
		if v, ok := lessonMeta["videoId"].(string); ok && v != "" {
			fmt.Fprintf(&b, "video_id: %q\n", v)
		}
		if v, ok := lessonMeta["videoLink"].(string); ok && v != "" {
			fmt.Fprintf(&b, "video_link: %q\n", v)
		}
		// videoLinkData.mux and attachments appear only in older/other-community lessons.
		if vld, ok := lessonMeta["videoLinkData"].(map[string]any); ok {
			if mux, ok := vld["mux"].(string); ok && mux != "" {
				fmt.Fprintf(&b, "mux_url: %q\n", mux)
			}
		}
		if v, ok := lessonMeta["attachments"].(string); ok && v != "" && v != "[]" {
			fmt.Fprintf(&b, "attachments: %s\n", v)
		}
		if v, ok := lessonMeta["resources"].(string); ok && v != "" && v != "[]" {
			fmt.Fprintf(&b, "resources: %s\n", v)
		}
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	if strings.TrimSpace(body) != "" {
		b.WriteString(body)
		b.WriteString("\n")
	} else {
		b.WriteString("_(no body content; this lesson may be video-only)_\n")
	}
	return b.String()
}

func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '/':
			b.WriteByte('-')
		}
	}
	s := b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}
