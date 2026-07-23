package exporter

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tr0sT/multica-declarative/internal/model"
	"gopkg.in/yaml.v3"
)

func makeRuntimeDocuments(runtimes []model.Runtime, agents []model.Agent) (map[string]string, map[string]runtimeDocument, error) {
	byID := map[string]model.Runtime{}
	for _, v := range runtimes {
		byID[v.ID] = v
	}
	usedIDs := map[string]struct{}{}
	for _, a := range agents {
		if strings.TrimSpace(a.RuntimeID) == "" {
			return nil, nil, fmt.Errorf("agent %q has no runtime_id", a.Name)
		}
		usedIDs[a.RuntimeID] = struct{}{}
	}
	ids := []string{}
	for id := range usedIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	aliases := map[string]string{}
	docs := map[string]runtimeDocument{}
	used := map[string]struct{}{}
	for _, id := range ids {
		v, ok := byID[id]
		if !ok {
			return nil, nil, fmt.Errorf("agent references runtime %q not returned by runtime list", id)
		}
		alias := uniqueSlug(runtimeDisplayName(v), v.ID, used)
		aliases[id] = alias
		docs[alias] = selectorForRuntime(v, runtimes)
	}
	return aliases, docs, nil
}
func selectorForRuntime(v model.Runtime, all []model.Runtime) runtimeDocument {
	if v.CustomName != "" && uniqueRuntimeMatch(all, func(x model.Runtime) bool { return x.CustomName == v.CustomName && x.Provider == v.Provider }) {
		return runtimeDocument{CustomName: v.CustomName, Provider: v.Provider}
	}
	if v.Name != "" && uniqueRuntimeMatch(all, func(x model.Runtime) bool { return x.Name == v.Name && x.Provider == v.Provider }) {
		return runtimeDocument{Name: v.Name, Provider: v.Provider}
	}
	return runtimeDocument{ID: v.ID}
}
func uniqueRuntimeMatch(items []model.Runtime, match func(model.Runtime) bool) bool {
	count := 0
	for _, v := range items {
		if match(v) {
			count++
		}
	}
	return count == 1
}
func runtimeDisplayName(v model.Runtime) string {
	if strings.TrimSpace(v.CustomName) != "" {
		return v.CustomName
	}
	if strings.TrimSpace(v.Name) != "" {
		return v.Name
	}
	if strings.TrimSpace(v.Provider) != "" {
		return v.Provider
	}
	return "runtime"
}

func permissionForExport(a model.Agent) (any, error) {
	switch a.PermissionMode {
	case "", "private":
		return "private", nil
	case "public_to":
		p := permissionDocument{Mode: "public_to"}
		for _, t := range a.InvocationTargets {
			switch t.TargetType {
			case "workspace":
				p.Workspace = true
			case "member":
				if t.TargetID == nil || *t.TargetID == "" {
					return nil, fmt.Errorf("member target has no id")
				}
				p.Members = append(p.Members, *t.TargetID)
			case "team":
				return nil, fmt.Errorf("team-scoped permissions are not supported by the official CLI")
			default:
				return nil, fmt.Errorf("unsupported target type %q", t.TargetType)
			}
		}
		sort.Strings(p.Members)
		if p.Workspace && len(p.Members) == 0 {
			return "workspace", nil
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unsupported permission mode %q", a.PermissionMode)
	}
}
func normalizeSkillContent(skill model.Skill) (string, bool, error) {
	description := strings.TrimSpace(skill.Description)
	changed := false
	if description == "" {
		description = "Imported from Multica."
		changed = true
	}
	body, fm, valid := splitFrontmatter(skill.Content)
	if valid {
		name, _ := fm["name"].(string)
		desc, _ := fm["description"].(string)
		if strings.TrimSpace(name) == skill.Name && strings.TrimSpace(desc) == description {
			return skill.Content, changed, nil
		}
	}
	if fm == nil {
		fm = map[string]any{}
	}
	fm["name"] = skill.Name
	fm["description"] = description
	encoded, err := yaml.Marshal(fm)
	if err != nil {
		return "", false, err
	}
	content := "---\n" + string(encoded) + "---\n"
	if body != "" {
		content += "\n" + strings.TrimLeft(body, "\r\n")
	}
	return content, true, nil
}
func splitFrontmatter(content string) (string, map[string]any, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content, nil, false
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return content, nil, false
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &fm); err != nil {
		return strings.Join(lines[closing+1:], "\n"), nil, false
	}
	return strings.Join(lines[closing+1:], "\n"), fm, true
}
func validateSkillFiles(skill string, files []model.SkillFile) ([]model.SkillFile, error) {
	out := append([]model.SkillFile(nil), files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	seen := map[string]struct{}{}
	for i := range out {
		f := &out[i]
		if f.Content == "" || !utf8.ValidString(f.Content) {
			return nil, fmt.Errorf("skill %q contains invalid text file %q", skill, f.Path)
		}
		clean := path.Clean(strings.TrimSpace(f.Path))
		if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
			return nil, fmt.Errorf("skill %q contains unsafe file path %q", skill, f.Path)
		}
		if strings.EqualFold(clean, "SKILL.md") {
			return nil, fmt.Errorf("skill %q file conflicts with SKILL.md", skill)
		}
		if _, ok := seen[clean]; ok {
			return nil, fmt.Errorf("skill %q duplicate file %q", skill, clean)
		}
		seen[clean] = struct{}{}
		f.Path = clean
	}
	return out, nil
}
func normalizedConcurrency(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
func uniqueSlug(name, id string, used map[string]struct{}) string {
	base := slugify(name)
	if base == "" {
		base = "resource"
	}
	candidate := base
	if _, ok := used[candidate]; ok {
		suffix := shortID(id)
		if suffix == "" {
			suffix = "2"
		}
		candidate = base + "-" + suffix
		for i := 2; ; i++ {
			if _, ok := used[candidate]; !ok {
				break
			}
			candidate = fmt.Sprintf("%s-%s-%d", base, suffix, i)
		}
	}
	used[candidate] = struct{}{}
	return candidate
}
func slugify(value string) string {
	var b bytes.Buffer
	sep := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			sep = false
		} else if b.Len() > 0 && !sep {
			b.WriteByte('-')
			sep = true
		}
	}
	return strings.Trim(b.String(), "-")
}
func shortID(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), "-", "")
	if len(v) > 8 {
		v = v[:8]
	}
	return strings.ToLower(v)
}
func duplicateName[T any](items []T, name func(T) string) string {
	seen := map[string]struct{}{}
	for _, v := range items {
		n := name(v)
		if _, ok := seen[n]; ok {
			return n
		}
		seen[n] = struct{}{}
	}
	return ""
}
