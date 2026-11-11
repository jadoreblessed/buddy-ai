package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		t.Fatalf("статус %d, ожидался %d; ответ: %s",
			response.Code, want, response.Body.String())
	}
}

func TestRequestFlow(t *testing.T) {
	file := filepath.Join(t.TempDir(), "request.json")
	app, err := newApp(file)
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(app)

	before := send(handler, "GET", "/api/plan", "", "")
	requireStatus(t, before, http.StatusOK)

	var firstPlan Plan
	if err := json.Unmarshal(before.Body.Bytes(), &firstPlan); err != nil {
		t.Fatal(err)
	}
	if firstPlan.Progress != 2 || firstPlan.Request != nil {
		t.Fatalf("неверный начальный план: %+v", firstPlan)
	}

	forbiddenCreate := send(handler, "POST", "/api/requests", "mentor-1", "")
	requireStatus(t, forbiddenCreate, http.StatusForbidden)

	created := send(handler, "POST", "/api/requests", "intern-1", "")
	requireStatus(t, created, http.StatusCreated)

	var createdBody struct {
		Request Request `json:"request"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Request.ID != "DEMO-4906" || createdBody.Request.Status != "pending" {
		t.Fatalf("неверная заявка: %+v", createdBody.Request)
	}

	repeated := send(handler, "POST", "/api/requests", "intern-1", "")
	requireStatus(t, repeated, http.StatusOK)

	current := send(handler, "GET", "/api/requests/current", "", "")
	requireStatus(t, current, http.StatusOK)
	if !json.Valid(current.Body.Bytes()) {
		t.Fatal("заявка вернулась не в формате JSON")
	}

	forbiddenApprove := send(handler, "POST",
		"/api/requests/DEMO-4906/approve", "intern-1", "")
	requireStatus(t, forbiddenApprove, http.StatusForbidden)

	missing := send(handler, "POST",
		"/api/requests/DEMO-0000/approve", "mentor-1", "")
	requireStatus(t, missing, http.StatusNotFound)

	approved := send(handler, "POST",
		"/api/requests/DEMO-4906/approve", "mentor-1", "")
	requireStatus(t, approved, http.StatusOK)

	again := send(handler, "POST",
		"/api/requests/DEMO-4906/approve", "mentor-1", "")
	requireStatus(t, again, http.StatusOK)

	after := send(handler, "GET", "/api/plan", "", "")
	requireStatus(t, after, http.StatusOK)

	var finalPlan Plan
	if err := json.Unmarshal(after.Body.Bytes(), &finalPlan); err != nil {
		t.Fatal(err)
	}
	if finalPlan.Progress != 3 ||
		finalPlan.Steps[2].Status != "done" ||
		finalPlan.Steps[3].Status != "current" {
		t.Fatalf("план не обновился: %+v", finalPlan)
	}

	restarted, err := newApp(file)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.request == nil || restarted.request.Status != "approved" {
		t.Fatal("заявка не сохранилась после повторного открытия")
	}
}

func TestOriginAndMethods(t *testing.T) {
	app, err := newApp(filepath.Join(t.TempDir(), "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(app)

	requireStatus(t, send(handler, "GET", "/health", "", ""), http.StatusOK)
	requireStatus(t, send(handler, "GET", "/api/plan", "", "http://other.test"),
		http.StatusForbidden)
	requireStatus(t, send(handler, "POST", "/api/plan", "", ""),
		http.StatusMethodNotAllowed)

	options := send(handler, "OPTIONS", "/api/requests", "",
		"http://localhost:5173")
	requireStatus(t, options, http.StatusNoContent)
	if options.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("браузеру не разрешён доступ к API")
	}
}

func TestStorageErrors(t *testing.T) {
	dir := t.TempDir()

	brokenJSON := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(brokenJSON, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newApp(brokenJSON); err == nil {
		t.Fatal("повреждённый файл должен вызывать ошибку")
	}

	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(blocked, "request.json")

	app := &App{file: badPath}
	create := send(newHandler(app), "POST", "/api/requests", "intern-1", "")
	requireStatus(t, create, http.StatusInternalServerError)
	if app.request != nil {
		t.Fatal("заявка не должна появиться, если её не удалось сохранить")
	}

	app.request = &Request{ID: "DEMO-4906", Status: "pending"}
	approve := send(newHandler(app), "POST",
		"/api/requests/DEMO-4906/approve", "mentor-1", "")
	requireStatus(t, approve, http.StatusInternalServerError)
	if app.request.Status != "pending" {
		t.Fatal("статус изменился без сохранения")
	}
}
