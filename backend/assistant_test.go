package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeModel struct {
	calls  int
	input  string
	output string
	err    error
}

func (m *fakeModel) Generate(_ context.Context, _, input string) (string, error) {
	m.calls++
	m.input = input
	return m.output, m.err
}

func postBuddy(handler http.Handler, question string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"question": question})
	request := httptest.NewRequest(http.MethodPost, "/api/buddy/answer", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func testCatalog(t *testing.T) *MaterialCatalog {
	t.Helper()
	catalog, err := loadDefaultMaterialCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func decodeMaterials(t *testing.T, response *httptest.ResponseRecorder) []Material {
	t.Helper()
	var payload struct {
		Materials []Material `json:"materials"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Materials
}

func requireVisibleSources(t *testing.T, sources []AnswerSource, materials []Material) {
	t.Helper()
	for _, source := range sources {
		found := false
		for _, material := range materials {
			if source.ID == material.ID && source.Title == material.Title &&
				source.Excerpt != "" && strings.Contains(material.Content, source.Excerpt) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("источник не найден в видимых материалах: %+v", source)
		}
	}
}

func TestBuddyAnswerUsesAssignedPlanAndTestSource(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	source := &mutableSource{plan: backend}
	model := &fakeModel{output: "Создай заявку на backend-staging и дождись наставника. [demo-access-policy]"}
	handler := newHandler(&App{
		source: source, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t),
		assistant: &Assistant{model: model, modelName: "test-model"},
	})

	response := postBuddy(handler, "Какие доступы мне нужны?")
	requireStatus(t, response, http.StatusOK)
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Model != "test-model" || len(answer.Sources) != 2 ||
		!strings.Contains(answer.Sources[1].Excerpt, "backend-staging") || model.calls != 1 {
		t.Fatalf("ответ не привязан к тестовой инструкции и назначенному плану: %+v", answer)
	}
	if !strings.Contains(model.input, "Backend-стажёр") || !strings.Contains(model.input, "backend-staging") {
		t.Fatalf("модели передан неверный контекст: %s", model.input)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))

	source.plan = analyst
	model.output = "Создай заявку на metrics. [demo-access-policy]"
	response = postBuddy(handler, "Какие доступы мне нужны?")
	requireStatus(t, response, http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.Sources[1].Excerpt, "metrics") || strings.Contains(model.input, "backend-staging") {
		t.Fatalf("ответ сохранил данные старой позиции: %+v", answer)
	}
	requireVisibleSources(t, answer.Sources, decodeMaterials(t, send(handler, "GET", "/api/materials", "", "")))
}

func TestBuddyUnknownQuestionDoesNotInventAnswerOrCallModel(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	model := &fakeModel{output: "Выдуманный регламент"}
	handler := newHandler(&App{
		source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t),
		assistant: &Assistant{model: model, modelName: "test-model"},
	})
	response := postBuddy(handler, "Как оформить отгул?")
	requireStatus(t, response, http.StatusOK)
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if model.calls != 0 || answer.Model != "" || len(answer.Sources) != 0 ||
		!strings.Contains(answer.Answer, "нет ответа") {
		t.Fatalf("неизвестный вопрос выдан за ответ модели: %+v", answer)
	}
}

func TestBuddyProviderErrorsAndValidation(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	app := &App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t)}
	handler := newHandler(app)
	requireStatus(t, postBuddy(handler, ""), http.StatusBadRequest)
	requireStatus(t, postBuddy(handler, strings.Repeat("а", 501)), http.StatusBadRequest)
	app.assistant = &Assistant{model: NewOpenAIClient("https://api.openai.com/v1", "gpt-5.6-luna", ""), modelName: "gpt-5.6-luna"}
	response := postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusServiceUnavailable)
	if strings.Contains(response.Body.String(), "gpt-5.6-luna") {
		t.Fatal("не настроенная модель не должна выдавать ответ")
	}
	app.assistant.model = &fakeModel{err: ErrAIRateLimited}
	requireStatus(t, postBuddy(handler, "Какие доступы нужны?"), http.StatusServiceUnavailable)
	app.assistant.model = &fakeModel{err: errors.New("provider failed")}
	requireStatus(t, postBuddy(handler, "Какие доступы нужны?"), http.StatusBadGateway)
}

func TestAIConfigReadsLocalKeyAndAllowsEnvironmentOverride(t *testing.T) {
	t.Setenv("AI_API_KEY", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# local test\nAI_API_KEY=test-key\nAI_MODEL=gpt-5.6-luna\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadAIConfig(path)
	if err != nil || config.Key != "test-key" || config.Model != "gpt-5.6-luna" ||
		config.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("локальные настройки не загрузились: %+v, %v", config, err)
	}
	t.Setenv("AI_MODEL", "reviewer-model")
	config, err = loadAIConfig(path)
	if err != nil || config.Model != "reviewer-model" {
		t.Fatalf("переменная окружения не переопределила модель: %+v, %v", config, err)
	}
}
