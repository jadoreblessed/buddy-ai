package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func demoPost(handler http.Handler, path, user, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("X-Demo-User", user)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestDemoAbsenceAndMentorConsumerContracts(t *testing.T) {
	backend, analyst := samplePlans()
	source := &mutableSource{plan: backend}
	var absences []AbsenceRequest
	var messages []Message
	var absenceCreates, messageCreates int
	var failAbsences, failMessages bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /requests/current", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"request": nil})
	})
	mux.HandleFunc("GET /absences", func(w http.ResponseWriter, r *http.Request) {
		if failAbsences {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-Demo-User") != demoInternID {
			t.Error("wrong absence read role")
		}
		if r.URL.Query().Get("internId") != demoInternID {
			t.Errorf("wrong intern filter: %s", r.URL.RawQuery)
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": absences})
	})
	mux.HandleFunc("POST /absences", func(w http.ResponseWriter, r *http.Request) {
		if failAbsences {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-Demo-User") != demoInternID {
			t.Error("wrong absence role")
		}
		var body struct {
			Date string `json:"date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		absenceCreates++
		for _, item := range absences {
			if item.Date == body.Date {
				writeJSON(w, http.StatusOK, map[string]any{"request": item})
				return
			}
		}
		item := AbsenceRequest{ID: "ABS-1", InternID: demoInternID, Date: body.Date, Status: "pending", CreatedAt: "2026-11-15T12:00:00Z"}
		absences = append(absences, item)
		writeJSON(w, http.StatusCreated, map[string]any{"request": item})
	})
	mux.HandleFunc("POST /absences/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Demo-User") != source.plan.Mentor.ID {
			t.Error("wrong approval role")
		}
		for i := range absences {
			if absences[i].ID == r.PathValue("id") {
				absences[i].Status, absences[i].ApprovedAt = "approved", "2026-11-15T13:00:00Z"
				writeJSON(w, http.StatusOK, map[string]any{"request": absences[i]})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("GET /messages", func(w http.ResponseWriter, r *http.Request) {
		if failMessages {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-Demo-User") != demoInternID {
			t.Error("wrong message read role")
		}
		query := r.URL.Query()
		if query.Get("internId") != demoInternID || query.Get("mentorId") != source.plan.Mentor.ID || query.Get("planId") != source.plan.PlanID {
			t.Errorf("wrong message scope: %s", r.URL.RawQuery)
		}
		filtered := []Message{}
		for _, message := range messages {
			if message.PlanID == query.Get("planId") && message.MentorID == query.Get("mentorId") {
				filtered = append(filtered, message)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": filtered})
	})
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		if failMessages {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-Demo-User") != demoInternID {
			t.Error("wrong message role")
		}
		var input struct {
			PlanID   string `json:"planId"`
			MentorID string `json:"mentorId"`
			Text     string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.MentorID != source.plan.Mentor.ID || input.PlanID != source.plan.PlanID {
			t.Errorf("recipient/plan not assigned: %+v", input)
		}
		messageCreates++
		message := Message{ID: "MSG-" + string(rune('0'+messageCreates)), InternID: demoInternID,
			MentorID: input.MentorID, PlanID: input.PlanID, Text: input.Text, Status: "sent", CreatedAt: "2026-11-15T14:00:00Z"}
		messages = append(messages, message)
		writeJSON(w, http.StatusCreated, map[string]any{"message": message})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	handler := newHandler(&App{source: source, tickets: NewTicketClient(server.URL), absences: NewAbsenceClient(server.URL), messages: NewMessageClient(server.URL)})

	date := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	body := `{"date":"` + date + `"}`
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, body), http.StatusCreated)
	response := demoPost(handler, "/api/absences", demoInternID, body)
	requireStatus(t, response, http.StatusOK)
	if absenceCreates != 2 || len(absences) != 1 || !strings.Contains(response.Body.String(), "ABS-1") {
		t.Fatalf("duplicate absence receipt: %s", response.Body.String())
	}
	requireStatus(t, send(handler, "GET", "/api/absences?internId=somebody-else", "", ""), http.StatusOK)
	requireStatus(t, demoPost(handler, "/api/absences/ABS-1/approve", "mentor-1", ""), http.StatusOK)
	if absences[0].Status != "approved" || absences[0].ApprovedAt == "" {
		t.Fatalf("missing approval: %+v", absences[0])
	}
	requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, `{"text":"  Привет, наставник!  "}`), http.StatusCreated)
	if len(messages) != 1 || messages[0].Text != "Привет, наставник!" || messages[0].PlanID != backend.PlanID {
		t.Fatalf("wrong message: %+v", messages)
	}
	before := send(handler, "GET", "/api/mentor/messages?mentorId=other&planId=other", "", "")
	requireStatus(t, before, http.StatusOK)
	if !strings.Contains(before.Body.String(), "MSG-1") || !strings.Contains(before.Body.String(), backend.Mentor.Name) {
		t.Fatalf("missing receipt or mentor: %s", before.Body.String())
	}

	analyst.Mentor = Mentor{ID: "mentor-2", Name: "Елена Волкова", Channel: "Тестовый чат"}
	source.plan = analyst
	after := send(handler, "GET", "/api/mentor/messages?mentorId=mentor-1", "", "")
	requireStatus(t, after, http.StatusOK)
	if strings.Contains(after.Body.String(), "MSG-1") || !strings.Contains(after.Body.String(), "Елена Волкова") {
		t.Fatalf("plan switch leaked prior conversation: %s", after.Body.String())
	}
	requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, `{"text":"Новая команда"}`), http.StatusCreated)
	if messages[1].MentorID != "mentor-2" || messages[1].PlanID != analyst.PlanID {
		t.Fatalf("recipient did not switch with source: %+v", messages[1])
	}
	requireStatus(t, demoPost(handler, "/api/absences/ABS-1/approve", "mentor-1", ""), http.StatusForbidden)
	requireStatus(t, demoPost(handler, "/api/absences/ABS-1/approve", "mentor-2", ""), http.StatusOK)

	for _, input := range []string{`{"text":"hi","mentorId":"mentor-1"}`, `{"text":"hi","planId":"backend-v1"}`, `{"text":"  "}`, `{"text":"` + strings.Repeat("ж", 501) + `"}`} {
		requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, input), http.StatusBadRequest)
	}
	requireStatus(t, demoPost(handler, "/api/mentor/messages", "mentor-2", `{"text":"hi"}`), http.StatusForbidden)
	requireStatus(t, demoPost(handler, "/api/absences", "mentor-2", body), http.StatusForbidden)
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, `{"date":"2020-01-01"}`), http.StatusBadRequest)
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, `{"date":"`+date+`","reason":"medical"}`), http.StatusBadRequest)
	if messageCreates != 2 || absenceCreates != 2 {
		t.Fatal("invalid requests reached mock service")
	}

	failMessages, failAbsences = true, true
	requireStatus(t, send(handler, "GET", "/api/absences", "", ""), http.StatusBadGateway)
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, body), http.StatusBadGateway)
	requireStatus(t, demoPost(handler, "/api/absences/ABS-1/approve", "mentor-2", ""), http.StatusBadGateway)
	requireStatus(t, send(handler, "GET", "/api/mentor/messages", "", ""), http.StatusBadGateway)
	requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, `{"text":"hi"}`), http.StatusBadGateway)
}

func TestMentorIdentityUsesSourcedPlanWithoutModel(t *testing.T) {
	backend, _ := samplePlans()
	source := &mutableSource{plan: backend}
	tickets := ticketTestServer(t)
	defer tickets.Close()
	model := &fakeModel{err: ErrAIUnconfigured}
	handler := newHandler(&App{source: source, tickets: NewTicketClient(tickets.URL), catalog: testCatalog(t), assistant: &Assistant{model: model}})
	for _, question := range []string{"Кто мой наставник?", "Кому написать?", "Как связаться с наставником?"} {
		response := postBuddy(handler, question)
		requireStatus(t, response, http.StatusOK)
		var answer BuddyAnswer
		if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		if answer.SuggestedAction != "message_mentor" || !strings.Contains(answer.Answer, backend.Mentor.Name) ||
			len(answer.Sources) != 1 || answer.Sources[0].ID != "assigned-plan:"+backend.PlanID || model.calls != 0 {
			t.Fatalf("unsourced or model-generated mentor: %+v calls=%d", answer, model.calls)
		}
	}
	backend.Mentor = Mentor{ID: "mentor-2", Name: "Елена Волкова", Channel: "Тестовый чат"}
	source.plan = backend
	response := postBuddy(handler, "Кто мой наставник?")
	requireStatus(t, response, http.StatusOK)
	if !strings.Contains(response.Body.String(), "Елена Волкова") || strings.Contains(response.Body.String(), "Алексей Морозов") || model.calls != 0 {
		t.Fatalf("mentor answer did not follow source: %s", response.Body.String())
	}
}

func TestAbsenceGuideIsSyntheticAndVisible(t *testing.T) {
	backend, _ := samplePlans()
	plan := Plan{PlanID: backend.PlanID, Position: backend.Position, Mentor: backend.Mentor}
	catalog := testCatalog(t)
	selection := catalog.selectForQuestion(plan, "Как оформить отгул?")
	if !selection.topics["absence"] || len(selection.sources) != 2 ||
		selection.sources[0].ID != "demo-absence-guide" ||
		!strings.Contains(selection.sources[0].Excerpt, "синтетический сценарий") ||
		!catalog.sourcesVisible(plan, selection.sources) {
		t.Fatalf("demo absence guide missing or incorrectly sourced: %+v", selection)
	}
}

func TestDemoRejectsInvalidUpstreamReceiptsAndDenials(t *testing.T) {
	backend, _ := samplePlans()
	status := http.StatusCreated
	mux := http.NewServeMux()
	mux.HandleFunc("GET /requests/current", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"request": nil})
	})
	mux.HandleFunc("POST /absences", func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusForbidden {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, map[string]any{"request": nil})
	})
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusForbidden {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, map[string]any{"message": Message{
			ID: "MSG-wrong", InternID: demoInternID, MentorID: "someone-else", PlanID: backend.PlanID,
			Text: "Здравствуйте", Status: "sent", CreatedAt: "2026-11-15T14:00:00Z",
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	handler := newHandler(&App{source: &mutableSource{plan: backend}, tickets: NewTicketClient(server.URL),
		absences: NewAbsenceClient(server.URL), messages: NewMessageClient(server.URL)})
	date := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	absence := `{"date":"` + date + `"}`
	message := `{"text":"Здравствуйте"}`
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, absence), http.StatusBadGateway)
	requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, message), http.StatusBadGateway)
	status = http.StatusForbidden
	requireStatus(t, demoPost(handler, "/api/absences", demoInternID, absence), http.StatusForbidden)
	requireStatus(t, demoPost(handler, "/api/mentor/messages", demoInternID, message), http.StatusForbidden)
}
