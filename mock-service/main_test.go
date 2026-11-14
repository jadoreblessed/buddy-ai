package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testService(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	requests := filepath.Join(dir, "request.json")
	intern := filepath.Join(dir, "intern.json")
	service, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	return service, requests, intern
}

func call(t *testing.T, service *Service, method, path, user string, body any, want int, result any) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("X-Demo-User", user)
	response := httptest.NewRecorder()
	service.handler().ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, response.Code, want, response.Body.String())
	}
	if result != nil {
		if err := json.Unmarshal(response.Body.Bytes(), result); err != nil {
			t.Fatal(err)
		}
	}
}

func planFor(t *testing.T, service *Service) Plan {
	t.Helper()
	var plan Plan
	call(t, service, http.MethodGet, "/interns/intern-1/onboarding-plan", "", nil, http.StatusOK, &plan)
	return plan
}

func accessStep(t *testing.T, plan Plan) (Step, int) {
	t.Helper()
	for index, step := range plan.Steps {
		if step.ActionType == "request_access" {
			if step.Resource == "" {
				t.Fatal("access step has no resource")
			}
			return step, index
		}
	}
	t.Fatal("plan has no access step")
	return Step{}, 0
}

func switchTo(t *testing.T, service *Service, position string) {
	t.Helper()
	var plan Plan
	call(t, service, http.MethodPut, "/demo/interns/intern-1/position", "", map[string]string{"position": position}, http.StatusOK, &plan)
	if plan.Position != position {
		t.Fatalf("switch returned position %q, want %q", plan.Position, position)
	}
}

func TestAssignedPlansSwitchAndRestart(t *testing.T) {
	service, requests, intern := testService(t)
	positions := []string{"backend", "frontend", "analyst"}
	indexes := []int{1, 3, 0}
	ids := make(map[string]bool)
	for index, position := range positions {
		if index > 0 {
			switchTo(t, service, position)
		}
		plan := planFor(t, service)
		step, at := accessStep(t, plan)
		if plan.InternID != internID || plan.Intern != "Маша Ковалёва" || plan.Position != position || plan.Role == "" || plan.Team == "" || at != indexes[index] {
			t.Fatalf("unexpected %s plan: %+v; access index %d", position, plan, at)
		}
		if ids[plan.PlanID] || ids[step.ID] {
			t.Fatalf("plan or access step ID reused: %s, %s", plan.PlanID, step.ID)
		}
		ids[plan.PlanID], ids[step.ID] = true, true
		for _, item := range plan.Steps {
			if item.ID == "" || item.Title == "" || item.Detail == "" {
				t.Fatalf("incomplete step: %+v", item)
			}
		}
	}
	restarted, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	if planFor(t, restarted).Position != "analyst" {
		t.Fatal("assigned position was not restored")
	}
	call(t, restarted, http.MethodGet, "/interns/unknown/onboarding-plan", "", nil, http.StatusNotFound, nil)
	call(t, restarted, http.MethodPut, "/demo/interns/unknown/position", "", map[string]string{"position": "backend"}, http.StatusNotFound, nil)
	call(t, restarted, http.MethodPut, "/demo/interns/intern-1/position", "", map[string]string{"position": "designer"}, http.StatusBadRequest, nil)
	if planFor(t, restarted).Position != "analyst" {
		t.Fatal("invalid switch changed assigned plan")
	}
}

func lookup(t *testing.T, service *Service, plan Plan, step Step) *Request {
	t.Helper()
	var response struct {
		Request *Request `json:"request"`
	}
	path := "/requests/current?internId=intern-1&planId=" + plan.PlanID + "&stepId=" + step.ID
	call(t, service, http.MethodGet, path, "", nil, http.StatusOK, &response)
	return response.Request
}

func create(t *testing.T, service *Service, plan Plan, step Step, want int) *Request {
	t.Helper()
	var response struct {
		Request *Request `json:"request"`
	}
	call(t, service, http.MethodPost, "/requests", internID, map[string]string{
		"planId": plan.PlanID, "stepId": step.ID, "resource": step.Resource,
	}, want, &response)
	return response.Request
}

func TestRequestsAreIsolatedByPlanAndPersist(t *testing.T) {
	service, requests, intern := testService(t)
	backend := planFor(t, service)
	backendStep, _ := accessStep(t, backend)
	if lookup(t, service, backend, backendStep) != nil {
		t.Fatal("expected no initial request")
	}
	first := create(t, service, backend, backendStep, http.StatusCreated)
	if first == nil || first.PlanID != backend.PlanID || first.StepID != backendStep.ID || first.InternID != internID || first.Status != "pending" || first.CreatedAt == "" {
		t.Fatalf("unexpected first request: %+v", first)
	}
	if duplicate := create(t, service, backend, backendStep, http.StatusOK); duplicate == nil || duplicate.ID != first.ID {
		t.Fatalf("duplicate created another request: %+v", duplicate)
	}
	var approved struct {
		Request *Request `json:"request"`
	}
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", "mentor-1", nil, http.StatusOK, &approved)
	if approved.Request == nil || approved.Request.Status != "approved" || approved.Request.ApprovedAt == "" {
		t.Fatalf("request not approved: %+v", approved.Request)
	}
	switchTo(t, service, "frontend")
	frontend := planFor(t, service)
	frontendStep, _ := accessStep(t, frontend)
	if lookup(t, service, frontend, frontendStep) != nil {
		t.Fatal("old plan approval leaked into new plan")
	}
	second := create(t, service, frontend, frontendStep, http.StatusCreated)
	if second == nil || second.ID == first.ID || second.Status != "pending" || second.Resource != frontendStep.Resource {
		t.Fatalf("new plan request not independent: %+v", second)
	}
	restarted, err := newService(requests, intern)
	if err != nil {
		t.Fatal(err)
	}
	if got := lookup(t, restarted, backend, backendStep); got == nil || got.Status != "approved" {
		t.Fatalf("approval not restored: %+v", got)
	}
	if got := lookup(t, restarted, frontend, frontendStep); got == nil || got.Status != "pending" {
		t.Fatalf("pending request not restored: %+v", got)
	}
	call(t, restarted, http.MethodPost, "/requests/"+second.ID+"/approve", "mentor-1", nil, http.StatusOK, &approved)
	if got := lookup(t, restarted, frontend, frontendStep); got == nil || got.Status != "approved" {
		t.Fatalf("approval not saved: %+v", got)
	}
}

func TestRequestRoleAndInputRestrictions(t *testing.T) {
	service, _, _ := testService(t)
	plan := planFor(t, service)
	step, _ := accessStep(t, plan)
	body := map[string]string{"planId": plan.PlanID, "stepId": step.ID, "resource": step.Resource}
	call(t, service, http.MethodPost, "/requests", "mentor-1", body, http.StatusForbidden, nil)
	body["resource"] = "unrelated"
	call(t, service, http.MethodPost, "/requests", internID, body, http.StatusBadRequest, nil)
	body["resource"] = step.Resource
	first := create(t, service, plan, step, http.StatusCreated)
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", internID, nil, http.StatusForbidden, nil)
	call(t, service, http.MethodPost, "/requests/missing/approve", "mentor-1", nil, http.StatusNotFound, nil)
	if got := lookup(t, service, plan, step); got == nil || got.Status != "pending" {
		t.Fatalf("unauthorized action changed request: %+v", got)
	}
	call(t, service, http.MethodGet, "/requests/current?internId=unknown&planId="+plan.PlanID+"&stepId="+step.ID, "", nil, http.StatusNotFound, nil)
}

// callRaw exercises the public HTTP boundary without turning malformed JSON into
// a valid encoding through the usual call helper.
func callRaw(t *testing.T, service *Service, method, path, user, body string, want int) {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("X-Demo-User", user)
	response := httptest.NewRecorder()
	service.handler().ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s with %q: status %d, want %d; body: %s", method, path, body, response.Code, want, response.Body.String())
	}
}

func TestSavedStateRejectsCorruptionAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name, assignment, requests string
		directory                  string
	}{
		{name: "malformed assignment", assignment: `{"position":`},
		{name: "unknown saved position", assignment: `{"position":"designer"}`},
		{name: "malformed requests", requests: `{"not":"a request list"}`},
		{name: "assignment is not a file", directory: "assignment"},
		{name: "requests is not a file", directory: "requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			assignmentPath := filepath.Join(dir, "intern.json")
			requestPath := filepath.Join(dir, "requests.json")
			if tc.assignment != "" {
				if err := os.WriteFile(assignmentPath, []byte(tc.assignment), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.requests != "" {
				if err := os.WriteFile(requestPath, []byte(tc.requests), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.directory != "" {
				path := requestPath
				if tc.directory == "assignment" {
					path = assignmentPath
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := newService(requestPath, assignmentPath); err == nil {
				t.Fatal("service accepted invalid or unreadable saved state")
			}
		})
	}
}

func TestPositionSaveFailureDoesNotChangeAssignment(t *testing.T) {
	service, requests, assignment := testService(t)
	if err := os.Mkdir(assignment, 0700); err != nil {
		t.Fatal(err)
	}
	call(t, service, http.MethodPut, "/demo/interns/intern-1/position", "", map[string]string{"position": "frontend"}, http.StatusInternalServerError, nil)
	if got := planFor(t, service).Position; got != "backend" {
		t.Fatalf("failed save changed assigned position to %q", got)
	}
	if err := os.Remove(assignment); err != nil {
		t.Fatal(err)
	}
	restarted, err := newService(requests, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if got := planFor(t, restarted).Position; got != "backend" {
		t.Fatalf("failed assignment survived restart: %q", got)
	}
	switchTo(t, restarted, "frontend")
	again, err := newService(requests, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if got := planFor(t, again).Position; got != "frontend" {
		t.Fatalf("successful retry was not durable: %q", got)
	}
}

func TestRequestSaveFailuresLeaveNoPartialTransition(t *testing.T) {
	service, requests, assignment := testService(t)
	plan := planFor(t, service)
	step, _ := accessStep(t, plan)
	if err := os.Mkdir(requests, 0700); err != nil {
		t.Fatal(err)
	}
	if got := create(t, service, plan, step, http.StatusInternalServerError); got != nil {
		t.Fatalf("failed create returned a request: %+v", got)
	}
	if got := lookup(t, service, plan, step); got != nil {
		t.Fatalf("failed create became visible: %+v", got)
	}
	if err := os.Remove(requests); err != nil {
		t.Fatal(err)
	}
	pending := create(t, service, plan, step, http.StatusCreated)
	if pending == nil || pending.Status != "pending" {
		t.Fatalf("retry did not create a pending request: %+v", pending)
	}
	if err := os.Remove(requests); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(requests, 0700); err != nil {
		t.Fatal(err)
	}
	call(t, service, http.MethodPost, "/requests/"+pending.ID+"/approve", "mentor-1", nil, http.StatusInternalServerError, nil)
	if got := lookup(t, service, plan, step); got == nil || got.Status != "pending" || got.ApprovedAt != "" {
		t.Fatalf("failed approval changed visible request: %+v", got)
	}
	if err := os.Remove(requests); err != nil {
		t.Fatal(err)
	}
	var approved struct {
		Request *Request `json:"request"`
	}
	call(t, service, http.MethodPost, "/requests/"+pending.ID+"/approve", "mentor-1", nil, http.StatusOK, &approved)
	if approved.Request == nil || approved.Request.Status != "approved" || approved.Request.ApprovedAt == "" {
		t.Fatalf("approval retry failed: %+v", approved.Request)
	}
	restarted, err := newService(requests, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if got := lookup(t, restarted, plan, step); got == nil || got.ID != pending.ID || got.Status != "approved" || got.ApprovedAt != approved.Request.ApprovedAt {
		t.Fatalf("approval retry was not durable: %+v", got)
	}
}

func TestMalformedJSONAndMismatchedRequestTargets(t *testing.T) {
	service, _, _ := testService(t)
	plan := planFor(t, service)
	step, _ := accessStep(t, plan)
	for _, body := range []string{`{"position":`, `{"position":42}`, `{"position":"frontend","unknown":true}`, `{"position":"frontend"} {}`} {
		callRaw(t, service, http.MethodPut, "/demo/interns/intern-1/position", "", body, http.StatusBadRequest)
	}
	if got := planFor(t, service).Position; got != plan.Position {
		t.Fatalf("malformed position changed assignment: %q", got)
	}
	for _, body := range []string{`{"planId":`, `{"planId":42}`, `{"planId":"x","unknown":true}`, `{"planId":"x"} {}`} {
		callRaw(t, service, http.MethodPost, "/requests", internID, body, http.StatusBadRequest)
	}
	valid := map[string]string{"planId": plan.PlanID, "stepId": step.ID, "resource": step.Resource}
	call(t, service, http.MethodPost, "/requests", "", valid, http.StatusForbidden, nil)
	call(t, service, http.MethodPost, "/requests", "other-intern", valid, http.StatusForbidden, nil)
	valid["planId"] = "unknown-plan"
	call(t, service, http.MethodPost, "/requests", internID, valid, http.StatusBadRequest, nil)
	valid["planId"] = plan.PlanID
	valid["stepId"] = plan.Steps[0].ID
	if valid["stepId"] == step.ID {
		valid["stepId"] = "unknown-step"
	}
	call(t, service, http.MethodPost, "/requests", internID, valid, http.StatusBadRequest, nil)
	call(t, service, http.MethodGet, "/requests/current?internId=intern-1&planId="+plan.PlanID+"&stepId=unknown-step", "", nil, http.StatusBadRequest, nil)
	if got := lookup(t, service, plan, step); got != nil {
		t.Fatalf("invalid requests created a resource: %+v", got)
	}
}

func TestOnlyAssignedPlanResourceCanBeRequestedAndApprovalIsIdempotent(t *testing.T) {
	service, requests, assignment := testService(t)
	backend := planFor(t, service)
	backendStep, _ := accessStep(t, backend)
	switchTo(t, service, "frontend")
	frontend := planFor(t, service)
	frontendStep, _ := accessStep(t, frontend)
	old := map[string]string{"planId": backend.PlanID, "stepId": backendStep.ID, "resource": backendStep.Resource}
	call(t, service, http.MethodPost, "/requests", internID, old, http.StatusBadRequest, nil)
	current := map[string]string{"planId": frontend.PlanID, "stepId": frontendStep.ID, "resource": backendStep.Resource}
	call(t, service, http.MethodPost, "/requests", internID, current, http.StatusBadRequest, nil)
	if got := lookup(t, service, frontend, frontendStep); got != nil {
		t.Fatalf("request with another plan's resource became visible: %+v", got)
	}
	first := create(t, service, frontend, frontendStep, http.StatusCreated)
	if first == nil || first.Resource != frontendStep.Resource || first.Status != "pending" {
		t.Fatalf("assigned resource request was not created: %+v", first)
	}
	if again := create(t, service, frontend, frontendStep, http.StatusOK); again == nil || *again != *first {
		t.Fatalf("repeated create did not return the same request: %+v; original: %+v", again, first)
	}
	var response struct {
		Request *Request `json:"request"`
	}
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", "", nil, http.StatusForbidden, nil)
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", "other-mentor", nil, http.StatusForbidden, nil)
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", "mentor-1", nil, http.StatusOK, &response)
	approved := response.Request
	if approved == nil || approved.Status != "approved" || approved.ApprovedAt == "" {
		t.Fatalf("approval not visible: %+v", approved)
	}
	var repeated struct {
		Request *Request `json:"request"`
	}
	call(t, service, http.MethodPost, "/requests/"+first.ID+"/approve", "mentor-1", nil, http.StatusOK, &repeated)
	if repeated.Request == nil || *repeated.Request != *approved {
		t.Fatalf("repeated approval changed request: %+v; original: %+v", repeated.Request, approved)
	}
	if again := create(t, service, frontend, frontendStep, http.StatusOK); again == nil || *again != *approved {
		t.Fatalf("repeated create after approval lost approval: %+v", again)
	}
	restarted, err := newService(requests, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if got := lookup(t, restarted, frontend, frontendStep); got == nil || *got != *approved {
		t.Fatalf("idempotent approval was not restored: %+v", got)
	}
}
