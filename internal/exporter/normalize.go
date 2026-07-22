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
	byID := make(map[string]model.Runtime, len(runtimes))
	for _, runtime := range runtimes {
		if runtime.ID != "" {
			byID[runtime.ID] = runtime
		}
	}
	used := make(map[string]struct{})
	for _, agent := range agents {
		if strings.TrimSpace(agent.RuntimeID) == "" {
			return nil, nil, fmt.Errorf("agent %q has no runtime_id", agent.Name)
		}
		used[agent.RuntimeID] = struct{}{}
	}
	ids := make([]string, 0, len(used))
	for id := range used {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, leftOK := byID[ids[i]]
		right, rightOK := byID[ids[j]]
		if leftOK && rightOK {
			leftName := runtimeDisplayName(left)
			rightName := runtimeDisplayName(right)
			if leftName != rightName {
				return leftName < rightName
			}
		}
		return ids[i] < ids[j]
	})

	aliases := make(map[string]string, len(ids))
	documents := make(map[string]runtimeDocument, len(ids))
	usedAliases := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		runtime, exists := byID[id]
		if !exists {
			return nil, nil, fmt.Errorf("agent references runtime %q which was not returned by runtime list", id)
		}
		alias := uniqueSlug(runtimeDisplayName(runtime), runtime.ID, usedAliases)
		aliases[id] = alias
		documents[alias] = selectorForRuntime(runtime, runtimes)
	}
	return aliases, documents, nil
}

func selectorForRuntime(runtime model.Runtime, all []model.Runtime) runtimeDocument {
	if runtime.CustomName != "" && uniqueRuntimeMatch(all, func(item model.Runtime) bool {
		return item.CustomName == runtime.CustomName && item.Provider == runtime.Provider
	}) {
		return runtimeDocument{CustomName: runtime.CustomName, Provider: runtime.Provider}
	}
	if runtime.Name != "" && uniqueRuntimeMatch(all, func(item model.Runtime) bool {
		return item.Name == runtime.Name && item.Provider == runtime.Provider
	}) {
		return runtimeDocument{Name: runtime.Name, Provider: runtime.Provider}
	}
	return runtimeDocument{ID: runtime.ID}
}

func uniqueRuntimeMatch(items []model.Runtime, match func(model.Runtime) bool) bool {
	count := 0
	for _, item := range items {
		if match(item) {
			count++
		}
	}
	return count == 1
}

func runtimeDisplayName(runtime model.Runtime) string {
	if strings.TrimSpace(runtime.CustomName) != "" {
		return runtime.CustomName
	}
	if strings.TrimSpace(runtime.Name) != "" {
		return runtime.Name
	}
	if strings.TrimSpace(runtime.Provider) != "" {
		return runtime.Provider
	}
	return "runtime"
}

func permissionForExport(agent model.Agent) (string, error) {
	switch agent.PermissionMode {
	case "", "private":
		return "private", nil
	case "public_to":
		for _, target := range agent.InvocationTargets {
			if target.TargetType == "workspace" {
				return "workspace", nil
			}
		}
		return "", fmt.Errorf("member- or team-scoped public_to permissions are not representable in v1alpha1")
	default:
		return "", fmt.Errorf("unsupported permission_mode %q", agent.PermissionMode)
	}
}

func normalizeSkillContent(skill model.Skill) (string, bool, error) {
	description := strings.TrimSpace(skill.Description)
	changed := false
	if description == "" {
		description = "Imported from Multica."
		changed = true
	}
	body, frontmatter, valid := splitFrontmatter(skill.Content)
	if valid {
		name, _ := frontmatter["name"].(string)
		descriptionValue, _ := frontmatter["description"].(string)
		if strings.TrimSpace(name) == skill.Name && strings.TrimSpace(descriptionValue) == description {
			return skill.Content, changed, nil
		}
	}
	if frontmatter == nil {
		frontmatter = make(map[string]any)
	}
	frontmatter["name"] = skill.Name
	frontmatter["description"] = description
	encoded, err := yaml.Marshal(frontmatter)
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
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return content, nil, false
	}
	frontmatter := make(map[string]any)
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &frontmatter); err != nil {
		return strings.Join(lines[closing+1:], "\n"), nil, false
	}
	return strings.Join(lines[closing+1:], "\n"), frontmatter, true
}

func validateSkillFiles(skillName string, files []model.SkillFile) ([]model.SkillFile, error) {
	result := append([]model.SkillFile(nil), files...)
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	seen := make(map[string]struct{}, len(result))
	for index := range result {
		file := &result[index]
		if file.Content == "" {
			return nil, fmt.Errorf("skill %q contains empty file %q, which v1alpha1 cannot represent", skillName, file.Path)
		}
		if !utf8.ValidString(file.Content) {
			return nil, fmt.Errorf("skill %q contains non-UTF-8 file %q", skillName, file.Path)
		}
		clean := path.Clean(strings.TrimSpace(file.Path))
		if clean == "." || clean == "" || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
			return nil, fmt.Errorf("skill %q contains unsafe file path %q", skillName, file.Path)
		}
		if strings.EqualFold(clean, "SKILL.md") {
			return nil, fmt.Errorf("skill %q contains additional file path %q which conflicts with SKILL.md", skillName, file.Path)
		}
		if _, exists := seen[clean]; exists {
			return nil, fmt.Errorf("skill %q contains duplicate file path %q", skillName, clean)
		}
		seen[clean] = struct{}{}
		file.Path = clean
	}
	return result, nil
}

func normalizedConcurrency(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func uniqueSlug(name, id string, used map[string]struct{}) string {
	base := slugify(name)
	if base == "" {
		base = "resource"
	}
	candidate := base
	if _, exists := used[candidate]; exists {
		suffix := shortID(id)
		if suffix == "" {
			suffix = "2"
		}
		candidate = base + "-" + suffix
		for index := 2; ; index++ {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = fmt.Sprintf("%s-%s-%d", base, suffix, index)
		}
	}
	used[candidate] = struct{}{}
	return candidate
}

func slugify(value string) string {
	var buffer bytes.Buffer
	lastSeparator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			buffer.WriteRune(character)
			lastSeparator = false
			continue
		}
		if buffer.Len() > 0 && !lastSeparator {
			buffer.WriteByte('-')
			lastSeparator = true
		}
	}
	return strings.Trim(buffer.String(), "-")
}

func shortID(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(value) > 8 {
		value = value[:8]
	}
	return strings.ToLower(value)
}

func duplicateName[T any](items []T, name func(T) string) string {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		value := name(item)
		if _, exists := seen[value]; exists {
			return value
		}
		seen[value] = struct{}{}
	}
	return ""
}
