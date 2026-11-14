package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
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
		Steps: []AssignedStep{
			{ID: "intro", Title: "Знакомство", ActionType: "none", Completed: true},
			{ID: "backend-access", Title: "Доступ к стенду", ActionType: "request_access", Resource: "backend-staging"},
			{ID: "task", Title: "Первая задача", ActionType: "none"},
		},
	}
	analyst := AssignedPlan{
		PlanID: "analyst-v1", InternID: demoInternID, Intern: "Маша", Position: "analyst",
		Role: "Аналитик", Team: "Аналитика",
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
