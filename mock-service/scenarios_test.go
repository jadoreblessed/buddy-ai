package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scenarioService(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	requests := filepath.Join(dir, "request.json")
	intern := filepath.Join(dir, "intern.json")
	s, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	return s, requests, intern
}
func scenarioCall(t *testing.T, s *Service, method, path, user, body string, status int) map[string]json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Demo-User", user)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != status {
		t.Fatalf("%s %s got %d, want %d: %s", method, path, rec.Code, status, rec.Body.String())
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestAbsenceLifecycleAndPersistence(t *testing.T) {
	s, requests, intern := scenarioService(t)
	date := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	body := `{"date":"` + date + `"}`
	created := scenarioCall(t, s, "POST", "/absences", "intern-1", body, 201)
	var absence AbsenceRequest
	if err := json.Unmarshal(created["request"], &absence); err != nil {
		t.Fatal(err)
	}
	if absence.ID == "" || absence.Date != date || absence.Status != "pending" || absence.CreatedAt == "" {
		t.Fatalf("unexpected request: %+v", absence)
	}
	duplicate := scenarioCall(t, s, "POST", "/absences", "intern-1", body, 200)
	if string(duplicate["request"]) != string(created["request"]) {
		t.Fatal("duplicate changed request")
	}
	scenarioCall(t, s, "POST", "/absences/"+absence.ID+"/approve", "intern-1", "", 403)
	approved := scenarioCall(t, s, "POST", "/absences/"+absence.ID+"/approve", "mentor-1", "", 200)
	if err := json.Unmarshal(approved["request"], &absence); err != nil {
		t.Fatal(err)
	}
	if absence.Status != "approved" || absence.ApprovedAt == "" {
		t.Fatalf("unexpected approval: %+v", absence)
	}
	restored, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	listed := scenarioCall(t, restored, "GET", "/absences?internId=intern-1", "intern-1", "", 200)
	var entries []AbsenceRequest
	if err := json.Unmarshal(listed["requests"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != "approved" {
		t.Fatalf("persisted entries: %+v", entries)
	}
	scenarioCall(t, restored, "POST", "/absences/"+absence.ID+"/approve", "mentor-1", "", 200)
	scenarioCall(t, restored, "POST", "/absences/unknown/approve", "mentor-1", "", 404)
}
func TestAbsenceRejectsInvalidInputAndSaveFailure(t *testing.T) {
	s, _, _ := scenarioService(t)
	futureBody := `{"date":"` + time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02") + `"}`
	for _, date := range []string{"2020-01-01", "2026-02-30", "tomorrow", "2026-1-01"} {
		scenarioCall(t, s, "POST", "/absences", "intern-1", `{"date":"`+date+`"}`, 400)
	}
	scenarioCall(t, s, "POST", "/absences", "mentor-1", futureBody, 403)
	scenarioCall(t, s, "GET", "/absences?internId=intern-2", "intern-1", "", 403)
	scenarioCall(t, s, "GET", "/absences?internId=intern-1", "mentor-1", "", 403)
	s.absenceFile = filepath.Join(t.TempDir(), "cannot-replace")
	if err := os.Mkdir(s.absenceFile, 0700); err != nil {
		t.Fatal(err)
	}
	scenarioCall(t, s, "POST", "/absences", "intern-1", futureBody, 500)
	if len(s.absences) != 0 {
		t.Fatal("save failure mutated absences")
	}
}
func TestMessageDeliveryAndValidation(t *testing.T) {
	s, requests, intern := scenarioService(t)
	scenarioCall(t, s, "POST", "/messages", "mentor-1", `{"planId":"onboarding-backend-v1","mentorId":"mentor-1","text":"Привет"}`, 403)
	scenarioCall(t, s, "POST", "/messages", "intern-1", `{"planId":"onboarding-backend-v1","mentorId":"other","text":"Привет"}`, 400)
	scenarioCall(t, s, "POST", "/messages", "intern-1", `{"planId":"onboarding-frontend-v1","mentorId":"mentor-1","text":"Привет"}`, 400)
	scenarioCall(t, s, "POST", "/messages", "intern-1", `{"planId":"onboarding-backend-v1","mentorId":"mentor-1","text":"  "}`, 400)
	scenarioCall(t, s, "POST", "/messages", "intern-1", `{"planId":"onboarding-backend-v1","mentorId":"mentor-1","text":"`+strings.Repeat("я", 501)+`"}`, 400)
	scenarioCall(t, s, "GET", "/messages?internId=intern-1&mentorId=other&planId=onboarding-backend-v1", "intern-1", "", 400)
	scenarioCall(t, s, "GET", "/messages?internId=intern-1&mentorId=mentor-1&planId=onboarding-frontend-v1", "intern-1", "", 400)
	scenarioCall(t, s, "GET", "/messages?internId=intern-1&mentorId=mentor-1&planId=onboarding-backend-v1", "mentor-1", "", 403)
	sent := scenarioCall(t, s, "POST", "/messages", "intern-1", `{"planId":"onboarding-backend-v1","mentorId":"mentor-1","text":"  Привет, наставник!  "}`, 201)
	var message Message
	if err := json.Unmarshal(sent["message"], &message); err != nil {
		t.Fatal(err)
	}
	if message.ID == "" || message.PlanID != "onboarding-backend-v1" || message.Text != "Привет, наставник!" || message.Status != "sent" || message.CreatedAt == "" {
		t.Fatalf("unexpected message: %+v", message)
	}
	restored, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	listed := scenarioCall(t, restored, "GET", "/messages?internId=intern-1&mentorId=mentor-1&planId=onboarding-backend-v1", "intern-1", "", 200)
	var entries []Message
	if err := json.Unmarshal(listed["messages"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != message.ID {
		t.Fatalf("persisted messages: %+v", entries)
	}
	scenarioCall(t, restored, "PUT", "/demo/interns/intern-1/position", "intern-1", `{"position":"frontend"}`, 200)
	listed = scenarioCall(t, restored, "GET", "/messages?internId=intern-1&mentorId=mentor-1&planId=onboarding-frontend-v1", "intern-1", "", 200)
	if string(listed["messages"]) != "[]" {
		t.Fatalf("other position's thread leaked: %s", listed["messages"])
	}
	scenarioCall(t, restored, "POST", "/messages", "intern-1", `{"planId":"onboarding-backend-v1","mentorId":"mentor-1","text":"Old plan"}`, 400)
	scenarioCall(t, restored, "POST", "/messages", "intern-1", `{"planId":"onboarding-frontend-v1","mentorId":"mentor-1","text":"Frontend hello"}`, 201)
	restored.messageFile = filepath.Join(t.TempDir(), "cannot-replace")
	if err := os.Mkdir(restored.messageFile, 0700); err != nil {
		t.Fatal(err)
	}
	scenarioCall(t, restored, "POST", "/messages", "intern-1", `{"planId":"onboarding-frontend-v1","mentorId":"mentor-1","text":"Again"}`, 500)
	if len(restored.messages) != 2 {
		t.Fatal("save failure mutated messages")
	}
}
func TestScenarioCorruptedPersistence(t *testing.T) {
	for _, name := range []string{"absences.json", "messages.json"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"invalid":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := newService(filepath.Join(dir, "request.json"), filepath.Join(dir, "intern.json")); err == nil {
				t.Fatal("corrupt records accepted")
			}
		})
	}
}
