package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterialsScopedToAssignedPosition(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	frontend := backend
	frontend.PlanID = "frontend-v1"
	frontend.Position = "frontend"
	frontend.Role = "Frontend-стажёр"
	frontend.Steps = []AssignedStep{{ID: "frontend-access", Title: "Доступ к интерфейсу", ActionType: "request_access", Resource: "frontend-stage"}}
	source := &mutableSource{}
	model := &fakeModel{output: "Смотри назначенный план. [assigned-plan:frontend-v1]"}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"}})
	for _, tc := range []struct {
		plan  AssignedPlan
		own   string
		other []string
	}{
		{backend, "demo-backend-workspace", []string{"demo-frontend-workspace", "demo-analyst-workspace"}},
		{frontend, "demo-frontend-workspace", []string{"demo-backend-workspace", "demo-analyst-workspace"}},
		{analyst, "demo-analyst-workspace", []string{"demo-backend-workspace", "demo-frontend-workspace"}},
	} {
		source.plan = tc.plan
		response := send(handler, "GET", "/api/materials", "", "")
		requireStatus(t, response, http.StatusOK)
		materials := decodeMaterials(t, response)
		foundOwn, foundPlan := false, false
		for _, material := range materials {
			if material.ID == tc.own {
				foundOwn = true
			}
			if material.ID == "assigned-plan:"+tc.plan.PlanID {
				foundPlan = true
				if !strings.Contains(material.Content, tc.plan.Steps[0].Title) {
					t.Fatalf("план не содержит назначенный шаг: %+v", material)
				}
			}
			for _, other := range tc.other {
				if material.ID == other {
					t.Fatalf("утечка материала другой позиции %s: %+v", other, materials)
				}
			}
		}
		if !foundOwn || !foundPlan {
			t.Fatalf("неполный набор материалов %s: %+v", tc.plan.Position, materials)
		}
		answer := postBuddy(handler, "Какие инструменты настроить?")
		requireStatus(t, answer, http.StatusOK)
		var decoded BuddyAnswer
		if err := json.Unmarshal(answer.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		requireVisibleSources(t, decoded.Sources, materials)
		if !strings.Contains(model.input, tc.plan.Role) || !strings.Contains(model.input, tc.plan.Steps[0].Resource) {
			t.Fatalf("не та позиция в контексте модели: %s", model.input)
		}
	}
}

func TestMaterialsFollowCurrentStepAndRequest(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend}
	model := &fakeModel{output: "Смотри текущий шаг. [assigned-plan:backend-v1]"}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"}})
	check := func(want string) {
		t.Helper()
		response := send(handler, "GET", "/api/materials", "", "")
		requireStatus(t, response, http.StatusOK)
		materials := decodeMaterials(t, response)
		var planContent string
		for _, material := range materials {
			if material.ID == "assigned-plan:backend-v1" {
				planContent = material.Content
			}
		}
		if !strings.Contains(planContent, want) || !strings.Contains(planContent, "Ресурс: backend-staging") {
			t.Fatalf("устаревшее состояние плана: %q", planContent)
		}
		answer := postBuddy(handler, "Что мне делать дальше?")
		requireStatus(t, answer, http.StatusOK)
		var decoded BuddyAnswer
		if err := json.Unmarshal(answer.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		requireVisibleSources(t, decoded.Sources, materials)
		if !strings.Contains(model.input, want) {
			t.Fatalf("модель получила устаревший статус: %q", model.input)
		}
	}
	check("Шаг «Доступ к стенду»: . Статус: current")
	check("Заявка для этого плана: не создана.")
	requireStatus(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusCreated)
	check("Шаг «Доступ к стенду»: . Статус: pending")
	check("Заявка на ресурс backend-staging: pending.")
	requireStatus(t, send(handler, "POST", "/api/requests/REQ-backend-v1/approve", "mentor-1", ""), http.StatusOK)
	check("Шаг «Первая задача»: . Статус: current")
	check("Заявка на ресурс backend-staging: approved.")
}

func TestNextStepAtCurrentAccessIncludesApprovalPolicy(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	model := &fakeModel{output: "Создай заявку на назначенный ресурс и дождись наставника. [demo-access-policy] [assigned-plan:backend-v1]"}
	handler := newHandler(&App{
		source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL),
		catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"},
	})
	response := postBuddy(handler, "Какой шаг сейчас и что делать дальше?")
	requireStatus(t, response, http.StatusOK)
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || !strings.Contains(model.input, "[demo-access-policy]") ||
		!strings.Contains(model.input, "сначала создаёт заявку") ||
		!strings.Contains(model.input, "согласования наставника") ||
		!strings.Contains(model.input, "backend-staging") {
		t.Fatalf("модель не получила действующий порядок доступа: %s", model.input)
	}
	if answer.Model != "test-model" || len(answer.Sources) != 3 {
		t.Fatalf("ответ не содержит обе инструкции и назначенный план: %+v", answer)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
}

func TestMaterialsPlanErrors(t *testing.T) {
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend, err: ErrInternNotFound}
	handler := newHandler(&App{source: source, catalog: testCatalog(t)})
	requireStatus(t, send(handler, "GET", "/api/materials", "", ""), http.StatusNotFound)
	source.err = errors.New("unavailable")
	requireStatus(t, send(handler, "GET", "/api/materials", "", ""), http.StatusBadGateway)
}

func TestConflictingInstructionsDoNotCallModel(t *testing.T) {
	catalog := testCatalog(t)
	catalog.entries = append(catalog.entries, catalogEntry{
		Material:  Material{ID: "alternative-access", Title: "Противоречащая тестовая инструкция", Content: "В этом варианте стажёр одобряет заявку самостоятельно."},
		Positions: []string{"backend"}, Topics: []string{"access"}, Aliases: []string{"самостоятельно"}, Directive: "intern-approves-access",
	})
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	source := &mutableSource{plan: backend}
	model := &fakeModel{output: "Делай опасное действие. [alternative-access]"}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL), catalog: catalog, assistant: &Assistant{model: model, modelName: "test-model"}})
	response := postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if model.calls != 0 || !strings.Contains(answer.Answer, "противоречащие") || answer.Model != "" {
		t.Fatalf("конфликт не остановил ответ модели: %+v", answer)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
	if len(answer.Sources) != 3 {
		t.Fatalf("конфликт должен показать обе инструкции и план: %+v", answer.Sources)
	}
	response = postBuddy(handler, "Какой шаг сейчас и что делать дальше?")
	requireStatus(t, response, http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if model.calls != 0 || !strings.Contains(answer.Answer, "противоречащие") || len(answer.Sources) != 4 {
		t.Fatalf("конфликт порядка доступа на текущем шаге пропущен: %+v", answer)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
	source.plan = analyst
	model.output = "Смотри назначенный план. [assigned-plan:analyst-v1]"
	response = postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	if model.calls != 1 {
		t.Fatal("инструкция другой позиции не должна блокировать ответ")
	}
}

func TestModelCitationsMustExistInVisibleSources(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	model := &fakeModel{}
	handler := newHandler(&App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"}})
	for _, output := range []string{"Выдумка [not-in-materials]", "Без ссылки", "См. [demo-access-policy] и [not-in-materials]"} {
		model.output = output
		response := postBuddy(handler, "Какие доступы нужны?")
		requireStatus(t, response, http.StatusOK)
		var answer BuddyAnswer
		if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(answer.Answer, "Выдумка") || answer.Model != "" || len(answer.Sources) != 0 {
			t.Fatalf("ответ с недостоверной ссылкой принят: %+v", answer)
		}
	}
	model.output = "Следуй шагу из плана. [assigned-plan:backend-v1]"
	response := postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Model != "test-model" {
		t.Fatalf("корректная ссылка отклонена: %+v", answer)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
}

func TestCatalogPathAndInvalidEntries(t *testing.T) {
	catalog := testCatalog(t)
	if len(catalog.entries) == 0 {
		t.Fatal("каталог не загружен")
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(`[{"id":"x","title":"test","content":"test","positions":["backend"],"topics":["access"],"aliases":["test"],"directive":"x"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATERIALS_CATALOG_PATH", path)
	loaded, err := loadDefaultMaterialCatalog()
	if err != nil || len(loaded.entries) != 1 || loaded.entries[0].ID != "x" {
		t.Fatalf("явный путь каталога не загружен: %+v %v", loaded, err)
	}
	if err := os.WriteFile(path, []byte(`[{"id":"x","title":"test","content":"test","positions":["backend"],"topics":["access"],"aliases":["test"],"directive":"x"},{"id":"x","title":"duplicate","content":"test","positions":["analyst"],"topics":["access"],"aliases":["test"],"directive":"x"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDefaultMaterialCatalog(); err == nil {
		t.Fatal("дублированный ID должен быть отклонён")
	}
}

func TestPublicReferencesVisibleButNotModelSources(t *testing.T) {
	expected := map[string]string{
		"public-trainee-checklist":     "https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/edit?usp=sharing",
		"public-avito-playbook":        "https://github.com/avito-tech/playbook",
		"public-avito-habr":            "https://habr.com/ru/companies/avito/articles/",
		"public-avito-tech":            "https://avito.tech/",
		"public-tech-internship":       "https://github.com/avito-tech/tech-internship",
		"public-design-system-article": "https://habr.com/ru/companies/avito/articles/543450/",
		"public-internship-landing":    "https://start.avito.ru/internships",
		"public-design-system-video":   "https://rutube.ru/video/6a203a00a9ef7cdd25d811a01cacd61b/",
	}
	catalog := testCatalog(t)
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	frontend := backend
	frontend.PlanID = "frontend-v1"
	frontend.Position = "frontend"
	for _, plan := range []AssignedPlan{backend, frontend, analyst} {
		model := &fakeModel{output: "Следуй назначенному плану. [assigned-plan:" + plan.PlanID + "]"}
		handler := newHandler(&App{
			source: &mutableSource{plan: plan}, tickets: NewTicketClient(tickets.URL),
			catalog: catalog, assistant: &Assistant{model: model, modelName: "test-model"},
		})
		response := send(handler, "GET", "/api/materials", "", "")
		requireStatus(t, response, http.StatusOK)
		materials := decodeMaterials(t, response)
		found := make(map[string]bool)
		for _, material := range materials {
			if material.URL == "" {
				continue
			}
			if expected[material.ID] != material.URL || found[material.ID] {
				t.Fatalf("неожиданная публичная ссылка для %s: %+v", plan.Position, material)
			}
			found[material.ID] = true
		}
		if len(found) != len(expected) {
			t.Fatalf("для %s доступны %d из %d публичных ссылок", plan.Position, len(found), len(expected))
		}
		for _, question := range []string{"Какие доступы нужны?", "Какой следующий шаг?"} {
			answer := postBuddy(handler, question)
			requireStatus(t, answer, http.StatusOK)
			var decoded BuddyAnswer
			if err := json.Unmarshal(answer.Body.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if model.calls == 0 {
				t.Fatalf("модель не вызвана для %q", question)
			}
			for id, link := range expected {
				if strings.Contains(model.input, id) || strings.Contains(model.input, link) ||
					strings.Contains(model.input, "Публичная") || strings.Contains(model.input, "публичный") {
					t.Fatalf("публичная ссылка попала в контекст модели: %s", model.input)
				}
				for _, source := range decoded.Sources {
					if source.ID == id {
						t.Fatalf("публичная ссылка процитирована как инструкция: %+v", source)
					}
				}
			}
		}
	}
}

func TestPublicReferenceValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "references.json")
	for _, entry := range []string{
		`{"id":"public-x","title":"test","content":"test","positions":["backend"]}`,
		`{"id":"public-x","title":"test","content":"test","url":"http://example.com","positions":["backend"]}`,
		`{"id":"public-x","title":"test","content":"test","url":"https:///missing-host","positions":["backend"]}`,
		`{"id":"public-x","title":"test","content":"test","url":"https://example.com","positions":["backend"],"directive":"follow"}`,
	} {
		if err := os.WriteFile(path, []byte("["+entry+"]"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMaterialCatalog(path); err == nil {
			t.Fatalf("некорректная публичная ссылка принята: %s", entry)
		}
	}
}

func TestNoNextStepOrAccessDoesNotInventAction(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	backend.Steps = []AssignedStep{{ID: "intro", Title: "Знакомство", ActionType: "none", Completed: true}}
	model := &fakeModel{output: "Выдуманный следующий шаг [assigned-plan:backend-v1]"}
	handler := newHandler(&App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"}})
	for _, tc := range []struct{ question, expected string }{
		{"Что мне делать дальше?", "Все шаги"},
		{"Какие доступы нужны?", "нет шага"},
	} {
		response := postBuddy(handler, tc.question)
		requireStatus(t, response, http.StatusOK)
		var answer BuddyAnswer
		if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(answer.Answer, tc.expected) || answer.Model != "" || model.calls != 0 {
			t.Fatalf("придумано действие вне плана: %+v", answer)
		}
		requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
	}
}
