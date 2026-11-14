package main

import (
	"encoding/json"
	"fmt"
	"os"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

type Material struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url,omitempty"`
}

type catalogEntry struct {
	Material
	Positions []string `json:"positions"`
	Topics    []string `json:"topics"`
	Aliases   []string `json:"aliases"`
	Directive string   `json:"directive"`
}

type MaterialCatalog struct {
	entries []catalogEntry
}

func loadMaterialCatalog(path string) (*MaterialCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать каталог материалов %s: %w", path, err)
	}
	var entries []catalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("разобрать каталог материалов %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("пустой каталог материалов %s", path)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || entry.Title == "" || entry.Content == "" ||
			len(entry.Positions) == 0 || seen[entry.ID] || strings.HasPrefix(entry.ID, "assigned-plan:") {
			return nil, fmt.Errorf("некорректный материал %q в %s", entry.ID, path)
		}
		seen[entry.ID] = true
		if entry.URL != "" || strings.HasPrefix(entry.ID, "public-") {
			link, err := url.Parse(entry.URL)
			if err != nil || link.Scheme != "https" || link.Hostname() == "" || link.User != nil ||
				entry.Directive != "" || len(entry.Topics) != 0 || len(entry.Aliases) != 0 {
				return nil, fmt.Errorf("некорректная публичная ссылка материала %q в %s", entry.ID, path)
			}
		} else if entry.Directive == "" || len(entry.Topics) == 0 || len(entry.Aliases) == 0 {
			return nil, fmt.Errorf("некорректная инструкция материала %q в %s", entry.ID, path)
		}
		for _, part := range [][]string{entry.Positions, entry.Topics, entry.Aliases} {
			for _, value := range part {
				if strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("пустая область или поисковый термин материала %q", entry.ID)
				}
			}
		}
	}
	return &MaterialCatalog{entries: entries}, nil
}

// An explicit path is useful in deployments; the fallback works both from the
// repository root and from backend/ when running Go tests or a local server.
func loadDefaultMaterialCatalog() (*MaterialCatalog, error) {
	if path := os.Getenv("MATERIALS_CATALOG_PATH"); path != "" {
		return loadMaterialCatalog(path)
	}
	_, source, _, _ := runtime.Caller(0)
	paths := []string{
		"backend/materials.json", "materials.json",
		filepath.Join(filepath.Dir(source), "materials.json"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return loadMaterialCatalog(path)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("каталог материалов не найден: %s", paths)
}

func (c *MaterialCatalog) visible(plan Plan) []Material {
	materials := make([]Material, 0, len(c.entries)+1)
	for _, entry := range c.entries {
		for _, position := range entry.Positions {
			if position == plan.Position {
				materials = append(materials, entry.Material)
				break
			}
		}
	}
	materials = append(materials, assignedPlanMaterial(plan))
	return materials
}

func assignedPlanMaterial(plan Plan) Material {
	var content strings.Builder
	fmt.Fprintf(&content, "Назначенный тестовый план: %s. Позиция: %s. Роль: %s. Команда: %s. Прогресс: %d из %d шагов.\n",
		plan.PlanID, plan.Position, plan.Role, plan.Team, plan.Progress, len(plan.Steps))
	for _, step := range plan.Steps {
		fmt.Fprintf(&content, "Шаг «%s»: %s. Статус: %s.", step.Title, step.Detail, step.Status)
		if step.Resource != "" {
			fmt.Fprintf(&content, " Ресурс: %s.", step.Resource)
		}
		content.WriteByte('\n')
	}
	if plan.Request != nil {
		fmt.Fprintf(&content, "Заявка на ресурс %s: %s.\n", plan.Request.Resource, plan.Request.Status)
	} else {
		content.WriteString("Заявка для этого плана: не создана.\n")
	}
	return Material{ID: "assigned-plan:" + plan.PlanID, Title: "Назначенный тестовый план", Content: content.String()}
}

type materialSelection struct {
	sources  []AnswerSource
	topics   map[string]bool
	conflict bool
}

func (c *MaterialCatalog) selectForQuestion(plan Plan, question string) materialSelection {
	text := strings.ToLower(question)
	visible := c.visible(plan)
	selected := make([]catalogEntry, 0, len(c.entries))
	topics := make(map[string]bool)
	for _, entry := range c.entries {
		if entry.URL != "" {
			continue
		}
		for _, material := range visible {
			if material.ID != entry.ID {
				continue
			}
			for _, alias := range entry.Aliases {
				if strings.Contains(text, strings.ToLower(alias)) {
					selected = append(selected, entry)
					for _, topic := range entry.Topics {
						topics[topic] = true
					}
					break
				}
			}
			break
		}
	}
	// A question about the next step also needs the instructions governing that
	// step. Otherwise the model sees the access resource but not the approval flow.
	if topics["plan"] {
		for _, step := range plan.Steps {
			if (step.Status == "current" || step.Status == "pending") && step.ActionType == "request_access" {
				topics["access"] = true
				break
			}
		}
	}
	if len(topics) == 0 {
		return materialSelection{}
	}
	// Include every applicable instruction for the matched topic, including a
	// differently worded instruction that the question did not directly match.
	for _, entry := range c.entries {
		if entry.URL != "" {
			continue
		}
		if !intersects(entry.Topics, topics) || hasEntry(selected, entry.ID) {
			continue
		}
		for _, material := range visible {
			if material.ID == entry.ID {
				selected = append(selected, entry)
				break
			}
		}
	}
	directives := make(map[string]string)
	result := materialSelection{sources: make([]AnswerSource, 0, len(selected)+1), topics: topics}
	for _, entry := range selected {
		for _, topic := range entry.Topics {
			if !topics[topic] {
				continue
			}
			if previous := directives[topic]; previous != "" && previous != entry.Directive {
				result.conflict = true
			}
			directives[topic] = entry.Directive
		}
		result.sources = append(result.sources, AnswerSource{ID: entry.ID, Title: entry.Title, Excerpt: entry.Content})
	}
	planMaterial := visible[len(visible)-1]
	result.sources = append(result.sources, AnswerSource{ID: planMaterial.ID, Title: planMaterial.Title, Excerpt: planMaterial.Content})
	return result
}

func intersects(values []string, selected map[string]bool) bool {
	for _, value := range values {
		if selected[value] {
			return true
		}
	}
	return false
}

func hasEntry(entries []catalogEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func (c *MaterialCatalog) sourcesVisible(plan Plan, sources []AnswerSource) bool {
	visible := c.visible(plan)
	for _, source := range sources {
		found := false
		for _, material := range visible {
			if material.URL == "" && source.ID == material.ID && source.Title == material.Title && source.Excerpt != "" &&
				strings.Contains(material.Content, source.Excerpt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
