package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func send(handler http.Handler, method, path, user, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if user != "" {
		request.Header.Set("X-Demo-User", user)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("статус %d, ожидался %d; ответ: %s", response.Code, want, response.Body.String())
	}
}

type mutableSource struct {
	plan AssignedPlan
	err  error
}

func (s *mutableSource) GetPlan(_ context.Context, _ string) (AssignedPlan, error) {
	return s.plan, s.err
}

func samplePlans() (AssignedPlan, AssignedPlan) {
	backend := AssignedPlan{
		PlanID: "backend-v1", InternID: demoInternID, Intern: "Маша", Position: "backend",
		Role: "Backend-стажёр", Team: "Платформа",
		Mentor: Mentor{ID: "mentor-1", Name: "Алексей Морозов", Channel: "Демо-мессенджер"},
		Steps: []AssignedStep{
			{ID: "intro", Title: "Знакомство", ActionType: "none", Completed: true},
			{ID: "backend-access", Title: "Доступ к стенду", ActionType: "request_access", Resource: "backend-staging"},
			{ID: "task", Title: "Первая задача", ActionType: "none"},
		},
	}
	analyst := AssignedPlan{
		PlanID: "analyst-v1", InternID: demoInternID, Intern: "Маша", Position: "analyst",
		Role: "Аналитик", Team: "Аналитика",
		Mentor: Mentor{ID: "mentor-1", Name: "Алексей Морозов", Channel: "Демо-мессенджер"},
		Steps: []AssignedStep{
			{ID: "analyst-access", Title: "Доступ к метрикам", ActionType: "request_access", Resource: "metrics"},
			{ID: "report", Title: "Первый отчёт", ActionType: "none"},
		},
	}
	return backend, analyst
}

func ticketTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	requests := make(map[string]*Request)
	key := func(planID, stepID string) string { return planID + "/" + stepID }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /requests/current", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("internId") != demoInternID {
			t.Error("неверный стажёр в запросе заявки")
		}
		mu.Lock()
		defer mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]*Request{
			"request": requests[key(r.URL.Query().Get("planId"), r.URL.Query().Get("stepId"))],
		})
	})
	mux.HandleFunc("POST /requests", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PlanID   string `json:"planId"`
			StepID   string `json:"stepId"`
			Resource string `json:"resource"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("тело заявки: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Demo-User") != demoInternID {
			t.Error("заявка передана без роли стажёра")
		}
		mu.Lock()
		defer mu.Unlock()
		k := key(input.PlanID, input.StepID)
		if request := requests[k]; request != nil {
			writeJSON(w, http.StatusOK, map[string]*Request{"request": request})
			return
		}
		request := &Request{
			ID: "REQ-" + input.PlanID, InternID: demoInternID, PlanID: input.PlanID,
			StepID: input.StepID, Resource: input.Resource, Status: "pending",
		}
		requests[k] = request
		writeJSON(w, http.StatusCreated, map[string]*Request{"request": request})
	})
	mux.HandleFunc("POST /requests/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		for _, request := range requests {
			if request.ID == r.PathValue("id") {
				request.Status = "approved"
				writeJSON(w, http.StatusOK, map[string]*Request{"request": request})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	return httptest.NewServer(mux)
}

func decodePlan(t *testing.T, response *httptest.ResponseRecorder) Plan {
	t.Helper()
	var plan Plan
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func decodeCurrentRequest(t *testing.T, response *httptest.ResponseRecorder) *Request {
	t.Helper()
	requireStatus(t, response, http.StatusOK)
	var payload struct {
		Request *Request `json:"request"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Request
}

func requireAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	requireStatus(t, response, status)
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var actual string
	if len(payload) != 1 || json.Unmarshal(payload["error"], &actual) != nil || actual != message {
		t.Fatalf("ожидалась только ошибка %q, получено %s", message, response.Body.String())
	}
}

func TestCurrentRequestReflectsTicketLifecycleAndAssignedPlan(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	source := &mutableSource{plan: backend}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL)})

	if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request != nil {
		t.Fatalf("до создания заявки ожидалось null, получено %+v", request)
	}
	requireStatus(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusCreated)
	pending := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", ""))
	if pending == nil || pending.ID != "REQ-backend-v1" || pending.PlanID != backend.PlanID ||
		pending.StepID != "backend-access" || pending.Resource != "backend-staging" || pending.Status != "pending" {
		t.Fatalf("текущая заявка не отражает созданную: %+v", pending)
	}
	again := send(handler, "POST", "/api/requests", demoInternID, "")
	requireStatus(t, again, http.StatusOK)
	var reused struct {
		Request *Request `json:"request"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &reused); err != nil {
		t.Fatal(err)
	}
	if reused.Request == nil || reused.Request.ID != pending.ID || reused.Request.Status != "pending" {
		t.Fatalf("повторное создание должно вернуть существующую заявку: %s", again.Body.String())
	}

	requireStatus(t, send(handler, "POST", "/api/requests/"+pending.ID+"/approve", "mentor-1", ""), http.StatusOK)
	approved := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", ""))
	if approved == nil || approved.ID != pending.ID || approved.Status != "approved" {
		t.Fatalf("текущая заявка не отражает согласование: %+v", approved)
	}
	source.plan = analyst
	if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request != nil {
		t.Fatalf("заявка прежнего плана не должна отображаться в новом: %+v", request)
	}
}

func TestRequestRoutesRejectUnauthorizedAndNonAccessStepsWithoutMutation(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL)})

	for _, user := range []string{"", "mentor-1"} {
		requireAPIError(t, send(handler, "POST", "/api/requests", user, ""), http.StatusForbidden,
			"Заявку может создать только стажёр")
	}
	if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request != nil {
		t.Fatalf("неавторизованный вызов создал заявку: %+v", request)
	}
	requireStatus(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusCreated)
	for _, user := range []string{"", demoInternID} {
		requireAPIError(t, send(handler, "POST", "/api/requests/REQ-backend-v1/approve", user, ""),
			http.StatusForbidden, "Согласовать заявку может только наставник")
	}
	if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request == nil || request.Status != "pending" {
		t.Fatalf("неавторизованный вызов изменил заявку: %+v", request)
	}

	source.plan.PlanID = "backend-next-v2"
	source.plan.Steps[0].Completed = false
	requireAPIError(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusConflict,
		"На текущем шаге заявка на доступ не требуется")
	if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request != nil {
		t.Fatalf("недоступный шаг создал заявку: %+v", request)
	}
}

func TestRequestReadAndCreateFailClosedForUnavailableOrStaleAssignments(t *testing.T) {
	backend, _ := samplePlans()
	for _, tc := range []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"missing intern", ErrInternNotFound, http.StatusNotFound, "Стажёр не найден"},
		{"source unavailable", errors.New("private source outage"), http.StatusBadGateway,
			"Не удалось загрузить назначенный план или статус заявки"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &mutableSource{plan: backend, err: tc.err}
			handler := newHandler(&App{source: source})
			requireAPIError(t, send(handler, "GET", "/api/requests/current", "", ""), tc.status, tc.body)
			requireAPIError(t, send(handler, "POST", "/api/requests", demoInternID, ""), tc.status, tc.body)
		})
	}

	for _, tc := range []struct {
		name    string
		current func(http.ResponseWriter)
	}{
		{"ticket service unavailable", func(w http.ResponseWriter) {
			http.Error(w, "internal ticket outage", http.StatusServiceUnavailable)
		}},
		{"stale ticket belongs to previous plan", func(w http.ResponseWriter) {
			writeJSON(w, http.StatusOK, map[string]*Request{"request": {
				ID: "REQ-old", InternID: demoInternID, PlanID: "old-plan",
				StepID: "backend-access", Resource: "backend-staging", Status: "pending",
			}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates atomic.Int32
			tickets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/requests/current" {
					tc.current(w)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/requests" {
					creates.Add(1)
				}
				t.Errorf("неожиданный вызов сервиса заявок: %s %s", r.Method, r.URL)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer tickets.Close()
			handler := newHandler(&App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL)})
			for _, methodAndPath := range [][2]string{
				{"GET", "/api/requests/current"},
				{"POST", "/api/requests"},
			} {
				requireAPIError(t, send(handler, methodAndPath[0], methodAndPath[1], demoInternID, ""),
					http.StatusBadGateway, "Не удалось загрузить назначенный план или статус заявки")
			}
			if count := creates.Load(); count != 0 {
				t.Fatalf("при неверном статусе заявки нельзя создавать новую: %d вызовов", count)
			}
		})
	}
}

func TestTicketActionErrorsDoNotExposeUpstreamDetailsOrInventRequests(t *testing.T) {
	backend, _ := samplePlans()
	for _, tc := range []struct {
		name     string
		action   string
		upstream int
		payload  string
		want     int
		message  string
	}{
		{"create unavailable", "create", http.StatusServiceUnavailable, `{"error":"internal ticket outage"}`,
			http.StatusBadGateway, "Не удалось выполнить действие через сервис заявок"},
		{"create missing request", "create", http.StatusOK, `{"request":null}`,
			http.StatusBadGateway, "Не удалось выполнить действие через сервис заявок"},
		{"approve unknown ticket", "approve", http.StatusNotFound, `{"error":"private ticket ID"}`,
			http.StatusNotFound, "Заявка не найдена"},
		{"approve unavailable", "approve", http.StatusServiceUnavailable, `{"error":"internal ticket outage"}`,
			http.StatusBadGateway, "Не удалось выполнить действие через сервис заявок"},
		{"approve missing request", "approve", http.StatusOK, `{"request":null}`,
			http.StatusBadGateway, "Не удалось выполнить действие через сервис заявок"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actions atomic.Int32
			tickets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/requests/current":
					writeJSON(w, http.StatusOK, map[string]*Request{"request": nil})
				case r.Method == http.MethodPost &&
					((tc.action == "create" && r.URL.Path == "/requests") ||
						(tc.action == "approve" && r.URL.Path == "/requests/REQ-unknown/approve")):
					actions.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.upstream)
					_, _ = w.Write([]byte(tc.payload))
				default:
					t.Errorf("неожиданный вызов сервиса заявок: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer tickets.Close()
			handler := newHandler(&App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(tickets.URL)})
			path, user := "/api/requests", demoInternID
			if tc.action == "approve" {
				path, user = "/api/requests/REQ-unknown/approve", "mentor-1"
			}
			requireAPIError(t, send(handler, "POST", path, user, ""), tc.want, tc.message)
			if count := actions.Load(); count != 1 {
				t.Fatalf("ожидался один вызов сервиса заявок, получено %d", count)
			}
			if request := decodeCurrentRequest(t, send(handler, "GET", "/api/requests/current", "", "")); request != nil {
				t.Fatalf("неуспешное действие не должно создавать заявку: %+v", request)
			}
		})
	}
}

func TestBuddyRejectsMalformedQuestionsBeforeCallingProvider(t *testing.T) {
	backend, _ := samplePlans()
	model := &fakeModel{output: "Не должен вызываться"}
	handler := newHandler(&App{
		source: &mutableSource{plan: backend}, catalog: testCatalog(t),
		assistant: &Assistant{model: model, modelName: "test-model"},
	})
	for _, tc := range []struct {
		name, body, message string
	}{
		{"invalid JSON", `{"question":`, "Передай вопрос в формате JSON"},
		{"unknown field", `{"question":"Доступ?","admin":true}`, "Передай вопрос в формате JSON"},
		{"multiple JSON objects", `{"question":"Доступ?"} {"question":"Другое"}`, "Некорректный формат вопроса"},
		{"empty question", `{"question":"  "}`, "Вопрос должен содержать от 1 до 500 символов"},
		{"over 500 characters", `{"question":"` + strings.Repeat("а", 501) + `"}`, "Вопрос должен содержать от 1 до 500 символов"},
		{"oversized payload", `{"question":"` + strings.Repeat("a", 4100) + `"}`, "Передай вопрос в формате JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/buddy/answer", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			requireAPIError(t, response, http.StatusBadRequest, tc.message)
			if model.calls != 0 {
				t.Fatalf("некорректный вопрос достиг провайдера: %d вызовов", model.calls)
			}
		})
	}
}

func TestBuddyProviderAndPlanFailuresReturnOnlyPublicErrors(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend}
	model := &fakeModel{err: ErrAIRateLimited}
	handler := newHandler(&App{
		source: source, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t),
		assistant: &Assistant{model: model, modelName: "test-model"},
	})
	requireAPIError(t, postBuddy(handler, "Какие доступы нужны?"), http.StatusServiceUnavailable,
		"Лимит запросов к модели исчерпан. Попробуй позже.")
	model.err = errors.New("private provider credentials")
	requireAPIError(t, postBuddy(handler, "Какие доступы нужны?"), http.StatusBadGateway,
		"Модель недоступна. Попробуй позже.")
	if model.calls != 2 {
		t.Fatalf("валидный вопрос не дошёл до провайдера: %d вызовов", model.calls)
	}
	source.err = ErrInternNotFound
	requireAPIError(t, postBuddy(handler, "Какие доступы нужны?"), http.StatusNotFound, "Стажёр не найден")
	if model.calls != 2 {
		t.Fatalf("при ошибке назначенного плана провайдер не должен вызываться: %d", model.calls)
	}
}

func TestAssignedPlanChangesWithSourceAndTicket(t *testing.T) {
	tickets := ticketTestServer(t)
	defer tickets.Close()
	backend, analyst := samplePlans()
	source := &mutableSource{plan: backend}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL)})

	before := send(handler, "GET", "/api/plan", "", "")
	requireStatus(t, before, http.StatusOK)
	first := decodePlan(t, before)
	if first.Position != "backend" || first.Progress != 1 || first.Steps[1].Status != "current" ||
		first.Steps[1].Resource != "backend-staging" || first.Request != nil {
		t.Fatalf("неверный назначенный план: %+v", first)
	}
	requireStatus(t, send(handler, "POST", "/api/requests", "mentor-1", ""), http.StatusForbidden)
	requireStatus(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusCreated)
	pending := decodePlan(t, send(handler, "GET", "/api/plan", "", ""))
	if pending.Steps[1].Status != "pending" || pending.Steps[2].Status != "locked" || pending.Progress != 1 {
		t.Fatalf("заявка не перевела шаг в ожидание: %+v", pending)
	}
	requireStatus(t, send(handler, "POST", "/api/requests/REQ-backend-v1/approve", demoInternID, ""), http.StatusForbidden)
	requireStatus(t, send(handler, "POST", "/api/requests/REQ-backend-v1/approve", "mentor-1", ""), http.StatusOK)
	approved := decodePlan(t, send(handler, "GET", "/api/plan", "", ""))
	if approved.Progress != 2 || approved.Steps[1].Status != "done" || approved.Steps[2].Status != "current" {
		t.Fatalf("одобрение не разблокировало задачу: %+v", approved)
	}

	source.plan = analyst
	changed := decodePlan(t, send(handler, "GET", "/api/plan", "", ""))
	if changed.Position != "analyst" || changed.PlanID != "analyst-v1" || changed.Steps[0].Title != "Доступ к метрикам" ||
		changed.Steps[0].Status != "current" || changed.Progress != 0 || changed.Request != nil {
		t.Fatalf("старая заявка повлияла на новый план: %+v", changed)
	}
	requireStatus(t, send(handler, "POST", "/api/requests", demoInternID, ""), http.StatusCreated)
	analystPending := decodePlan(t, send(handler, "GET", "/api/plan", "", ""))
	if analystPending.Request == nil || analystPending.Request.Resource != "metrics" ||
		analystPending.Steps[0].Status != "pending" {
		t.Fatalf("заявка аналитика не связалась с первым шагом: %+v", analystPending)
	}
}

func TestPlanSourceErrorsDoNotInventFallback(t *testing.T) {
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend, err: ErrInternNotFound}
	handler := newHandler(&App{source: source})
	requireStatus(t, send(handler, "GET", "/api/plan", "", ""), http.StatusNotFound)
	source.err = errors.New("source unavailable")
	response := send(handler, "GET", "/api/plan", "", "")
	requireStatus(t, response, http.StatusBadGateway)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["steps"]; exists {
		t.Fatal("ошибка источника не должна возвращать план")
	}
}

func TestOriginAndMethods(t *testing.T) {
	handler := newHandler(&App{})
	requireStatus(t, send(handler, "GET", "/health", "", ""), http.StatusOK)
	requireStatus(t, send(handler, "GET", "/api/plan", "", "http://other.test"), http.StatusForbidden)
	requireStatus(t, send(handler, "POST", "/api/plan", "", ""), http.StatusMethodNotAllowed)
	options := send(handler, "OPTIONS", "/api/requests", "", "http://localhost:5173")
	requireStatus(t, options, http.StatusNoContent)
	if options.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("браузеру не разрешён доступ к API")
	}
	loopback := send(handler, "OPTIONS", "/api/requests", "", "http://127.0.0.1:5173")
	requireStatus(t, loopback, http.StatusNoContent)
	if loopback.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" {
		t.Fatal("браузеру на 127.0.0.1 не разрешён доступ к API")
	}
}

func TestHTTPOnboardingSource(t *testing.T) {
	backend, _ := samplePlans()
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/interns/intern-1/onboarding-plan" {
			t.Errorf("неверный маршрут источника: %s", r.URL.Path)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, http.StatusOK, backend)
	}))
	defer server.Close()
	source := NewHTTPOnboardingSource(server.URL)
	plan, err := source.GetPlan(context.Background(), demoInternID)
	if err != nil || plan.PlanID != backend.PlanID || plan.Steps[1].ID != "backend-access" {
		t.Fatalf("источник не вернул назначенные шаги: %+v, %v", plan, err)
	}
	status = http.StatusNotFound
	if _, err := source.GetPlan(context.Background(), demoInternID); !errors.Is(err, ErrInternNotFound) {
		t.Fatalf("ожидался отсутствующий стажёр, получили %v", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := source.GetPlan(context.Background(), demoInternID); err == nil {
		t.Fatal("недоступный источник не должен возвращать план")
	}
}
