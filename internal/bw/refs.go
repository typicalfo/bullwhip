package bw

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var headingLine = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)\s*#*\s*$`)
var explicitAnchor = regexp.MustCompile(`(?i)<a\s+[^>]*?(?:id|name)=["']([^"']+)["'][^>]*>\s*(?:</a>)?`)

func headingSlug(text string) string {
	text = explicitAnchor.ReplaceAllString(text, "")
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '\t' {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Only local, repository-contained Markdown fragments are read. References are
// data, never commands, network requests or paths outside this working tree.
func resolveRefs(value any, root string) any {
	v, ok := value.(View)
	if !ok {
		return value
	}
	path, anchor, fragment := strings.Cut(v.Ref, "#")
	if !fragment {
		return v
	}
	u, e := url.Parse(v.Ref)
	if e != nil || u.IsAbs() || u.Host != "" {
		return v
	}
	anchor, e = url.PathUnescape(anchor)
	if e != nil {
		v.Warning = "Invalid ref fragment"
		return v
	}
	warn := func(reason string) any { v.Warning = fmt.Sprintf("Cannot resolve %s: %s", v.Ref, reason); return v }
	if anchor == "" {
		return warn("empty anchor")
	}
	path, e = url.PathUnescape(path)
	if e != nil {
		return warn("invalid path")
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return warn("repository root is unavailable")
	}
	resolved, e := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(path)))
	if e != nil {
		return warn("file is unavailable in this working tree")
	}
	rel, e := filepath.Rel(root, resolved)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(path) {
		return warn("path leaves the repository")
	}
	data, e := os.ReadFile(resolved)
	if e != nil {
		return warn("cannot read file")
	}
	lines := strings.Split(string(data), "\n")
	start, level := -1, 7
	counts := map[string]int{}
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			marker := trim[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		heading := headingLine.FindStringSubmatch(line)
		if start >= 0 && level < 7 && len(heading) > 0 && len(heading[1]) <= level {
			return withSection(v, lines[start:i])
		}
		if start >= 0 {
			if len(heading) > 0 && level == 7 {
				level = len(heading[1])
			}
			continue
		}
		matched := false
		for _, m := range explicitAnchor.FindAllStringSubmatch(line, -1) {
			if m[1] == anchor {
				matched = true
			}
		}
		if len(heading) > 0 {
			slug := headingSlug(heading[2])
			n := counts[slug]
			counts[slug]++
			if n > 0 {
				slug += fmt.Sprintf("-%d", n)
			}
			if slug == anchor {
				matched = true
			}
		}
		if matched {
			start = i
			if len(heading) > 0 {
				level = len(heading[1])
			}
		}
	}
	if start < 0 {
		return warn("anchor is missing")
	}
	return withSection(v, lines[start:])
}
func withSection(v View, lines []string) View {
	v.Section = strings.TrimSpace(strings.Join(lines, "\n"))
	return v
}
