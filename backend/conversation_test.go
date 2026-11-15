package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func conversationHandler(t *testing.T) (http.Handler, *fakeModel) {
	t.Helper()
	tickets := ticketTestServer(t)
	t.Cleanup(tickets.Close)
	backend, _ := samplePlans()
	model := &fakeModel{output: "Следуй назначенному плану. [assigned-plan:backend-v1]"}
	handler := newHandler(&App{
		source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL),
		catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"},
	})
	return handler, model
}

func postBuddyBody(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/buddy/answer", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeBuddyAnswer(t *testing.T, response *httptest.ResponseRecorder) BuddyAnswer {
	t.Helper()
	var answer BuddyAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestBuddyNormalizesTruncatedCitations(t *testing.T) {
	sources := []AnswerSource{
		{ID: "demo-access-policy"},
		{ID: "assigned-plan:onboarding-backend-v1"},
	}
	answer, ok := normalizeCitations("Нужен доступ. [demo-access-policy:1] [assigned-plan:1]", sources)
	if !ok || answer != "Нужен доступ. [demo-access-policy] [assigned-plan:onboarding-backend-v1]" {
		t.Fatalf("сокращённые ID не нормализованы: %q, %v", answer, ok)
	}
	if _, ok := normalizeCitations("Ответ без источника", sources); !ok {
		t.Fatal("отсутствие цитат должно отклоняться отдельной проверкой")
	}
	if _, ok := normalizeCitations("Ссылка [unknown:1]", sources); ok {
		t.Fatal("неизвестный источник принят")
	}
}

func TestBuddyConversationStaysInScopeWithoutModel(t *testing.T) {
	handler, model := conversationHandler(t)
	for _, tc := range []struct {
		question    string
		want        string
		suggestions bool
	}{
		{"Привет", "Привет, Маша", true},
		{"Как дела?", "всё отлично", true},
		{"Что ты умеешь?", "Помогаю с адаптацией", true},
		{"Спасибо!", "Пожалуйста", true},
		{"Пока", "До встречи", false},
		{"Напиши калькулятор на Python", "вне моей специализации", true},
		{"Какие скидки в кофейне?", "нет ответа", true},
	} {
		t.Run(tc.question, func(t *testing.T) {
			response := postBuddy(handler, tc.question)
			requireStatus(t, response, http.StatusOK)
			answer := decodeBuddyAnswer(t, response)
			if !strings.Contains(answer.Answer, tc.want) || answer.Model != "" || len(answer.Sources) != 0 {
				t.Fatalf("ответ вне сценария: %+v", answer)
			}
			if tc.suggestions && len(answer.Suggestions) == 0 {
				t.Fatalf("нет быстрых тем для продолжения диалога: %+v", answer)
			}
		})
	}
	if model.calls != 0 {
		t.Fatalf("разговорный слой вызвал модель: %d", model.calls)
	}
}

func TestBuddyAnswerRewritesTruncatedCitations(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Создай заявку на стенд. [assigned-plan:1] [demo-access-policy:1]"
	response := postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if answer.Model != "test-model" || !strings.Contains(answer.Answer, "[assigned-plan:backend-v1]") ||
		!strings.Contains(answer.Answer, "[demo-access-policy]") || strings.Contains(answer.Answer, ":1]") {
		t.Fatalf("ответ сохранил сокращённые ID: %+v", answer)
	}
}

func TestBuddyTeamQuestionUsesAssignedPlanAndMaterial(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Твоя команда — «Платформа», роль — Backend-стажёр. [demo-team-guide] [assigned-plan:backend-v1]"
	response := postBuddy(handler, "Расскажи о команде")
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 1 || !strings.Contains(model.input, "Платформа") ||
		!strings.Contains(model.input, "[demo-team-guide]") {
		t.Fatalf("модель не получила контекст команды: %s", model.input)
	}
	if answer.Model != "test-model" || len(answer.Sources) != 2 {
		t.Fatalf("ответ не привязан к команде и плану: %+v", answer)
	}
}

func TestBuddyGreetingDoesNotHijackDomainQuestion(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Создай заявку на стенд и дождись наставника. [demo-access-policy]"
	response := postBuddy(handler, "Привет! Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 1 || answer.Model != "test-model" {
		t.Fatalf("приветствие перехватило предметный вопрос: %+v", answer)
	}
}

func TestBuddyHistoryIsValidatedAndPassedToModel(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Следующий шаг — доступ к стенду. [assigned-plan:backend-v1]"
	response := postBuddyBody(t, handler, `{"question":"А как это сделать?","history":[{"question":"Что дальше?","answer":"Сейчас шаг доступа."}]}`)
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 1 || !strings.Contains(model.input, "Предыдущий диалог") ||
		!strings.Contains(model.input, "Что дальше?") || !strings.Contains(model.input, "Сейчас шаг доступа.") {
		t.Fatalf("история диалога не передана модели: %s", model.input)
	}
	if answer.Model != "test-model" {
		t.Fatalf("ответ модели не отмечен: %+v", answer)
	}

	for _, body := range []string{
		`{"question":"Что дальше?","history":[{"question":"1","answer":"2"},{"question":"3","answer":"4"},{"question":"5","answer":"6"}]}`,
		`{"question":"Что дальше?","history":[{"question":"Вопрос","answer":"  "}]}`,
		`{"question":"Что дальше?","history":[{"question":"Вопрос","answer":"` + strings.Repeat("а", 701) + `"}]}`,
	} {
		requireStatus(t, postBuddyBody(t, handler, body), http.StatusBadRequest)
	}
	if model.calls != 1 {
		t.Fatalf("некорректная история дошла до модели: %d", model.calls)
	}
}

func TestBuddyHistoryAcceptsMaxCyrillicTurns(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Следующий шаг — доступ к стенду. [assigned-plan:backend-v1]"
	body, err := json.Marshal(map[string]any{
		"question": "Что дальше?",
		"history": []BuddyHistoryTurn{
			{Question: strings.Repeat("а", 500), Answer: strings.Repeat("б", 700)},
			{Question: strings.Repeat("в", 500), Answer: strings.Repeat("г", 700)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := postBuddyBody(t, handler, string(body))
	requireStatus(t, response, http.StatusOK)
	if model.calls != 1 || len(decodeBuddyAnswer(t, response).Sources) == 0 {
		t.Fatalf("предельная история не дошла до модели: %d", model.calls)
	}
}

func TestBuddyFollowUpKeepsPreviousTopic(t *testing.T) {
	handler, model := conversationHandler(t)
	model.output = "Заявку создаёт стажёр, затем её согласует наставник. [demo-access-policy]"
	response := postBuddyBody(t, handler, `{"question":"А подробнее?","history":[{"question":"Какие доступы нужны?","answer":"Создай заявку на стенд."}]}`)
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 1 || len(answer.Sources) == 0 || !strings.Contains(model.input, "Предыдущий диалог") {
		t.Fatalf("уточняющий вопрос потерял тему: %+v / %s", answer, model.input)
	}

	withoutHistory := postBuddy(handler, "А подробнее?")
	requireStatus(t, withoutHistory, http.StatusOK)
	if model.calls != 1 || !strings.Contains(decodeBuddyAnswer(t, withoutHistory).Answer, "нет ответа") {
		t.Fatalf("уточнение без истории не должно уходить в модель: %s", model.input)
	}
}

func TestBuddyCodeRequestDoesNotReachModel(t *testing.T) {
	handler, model := conversationHandler(t)
	response := postBuddy(handler, "Напиши код для доступа к API")
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 0 || !strings.Contains(answer.Answer, "вне моей специализации") {
		t.Fatalf("офтопик дошёл до модели: %+v", answer)
	}
}

type sequenceModel struct {
	calls   int
	inputs  []string
	outputs []string
}

func (m *sequenceModel) Generate(_ context.Context, _, input string) (string, error) {
	m.calls++
	m.inputs = append(m.inputs, input)
	if len(m.outputs) == 0 {
		return "", nil
	}
	output := m.outputs[0]
	m.outputs = m.outputs[1:]
	return output, nil
}

func TestBuddyRetriesUncitedAnswerOnce(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	model := &sequenceModel{outputs: []string{
		"Создай заявку на стенд без ссылки.",
		"Создай заявку на стенд. [demo-access-policy] [assigned-plan:backend-v1]",
	}}
	handler := newHandler(&App{
		source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL),
		catalog: testCatalog(t), assistant: &Assistant{model: model, modelName: "test-model"},
	})
	response := postBuddy(handler, "Какие доступы нужны?")
	requireStatus(t, response, http.StatusOK)
	answer := decodeBuddyAnswer(t, response)
	if model.calls != 2 || answer.Model != "test-model" || len(answer.Sources) != 2 {
		t.Fatalf("повтор после отклонённого ответа не сработал: %+v, calls=%d", answer, model.calls)
	}
	if !strings.Contains(model.inputs[1], "Предыдущий ответ отклонён") {
		t.Fatalf("повтор не содержит корректирующую инструкцию: %s", model.inputs[1])
	}
}
