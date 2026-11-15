package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed plans.json
var planFiles embed.FS

const internID = "intern-1"

type Step struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	ActionType string `json:"actionType"`
	Completed  bool   `json:"completed"`
	Resource   string `json:"resource,omitempty"`
}

type Mentor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

type Plan struct {
	PlanID   string `json:"planId"`
	InternID string `json:"internId"`
	Intern   string `json:"intern"`
	Position string `json:"position"`
	Role     string `json:"role"`
	Team     string `json:"team"`
	Mentor   Mentor `json:"mentor"`
	Steps    []Step `json:"steps"`
}

type Request struct {
	ID         string `json:"id"`
	InternID   string `json:"internId"`
	PlanID     string `json:"planId"`
	StepID     string `json:"stepId"`
	Resource   string `json:"resource"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type Service struct {
	mu          sync.Mutex
	plans       map[string]Plan
	position    string
	requests    []*Request
	requestFile string
	internFile  string
	absences    []*AbsenceRequest
	messages    []*Message
	absenceFile string
	messageFile string
}

func newService(requestFile, internFile string) (*Service, error) {
	data, err := planFiles.ReadFile("plans.json")
	if err != nil {
		return nil, fmt.Errorf("read plans: %w", err)
	}
	var plans []Plan
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, fmt.Errorf("decode plans: %w", err)
	}
	s := &Service{
		plans:       make(map[string]Plan, len(plans)),
		position:    "backend",
		requestFile: requestFile,
		internFile:  internFile,
		absenceFile: filepath.Join(filepath.Dir(requestFile), "absences.json"),
		messageFile: filepath.Join(filepath.Dir(requestFile), "messages.json"),
	}
	for _, plan := range plans {
		if plan.PlanID == "" || plan.InternID != internID || plan.Position == "" || len(plan.Steps) == 0 || plan.Mentor.ID == "" || plan.Mentor.Name == "" || plan.Mentor.Channel == "" {
			return nil, fmt.Errorf("invalid plan for position %q", plan.Position)
		}
		if _, exists := s.plans[plan.Position]; exists {
			return nil, fmt.Errorf("duplicate plan for position %q", plan.Position)
		}
		s.plans[plan.Position] = plan
	}
	if _, ok := s.plans[s.position]; !ok {
		return nil, errors.New("default backend plan is missing")
	}

	data, err = os.ReadFile(internFile)
	if err == nil {
		var assignment struct {
			Position string `json:"position"`
		}
		if err := json.Unmarshal(data, &assignment); err != nil {
			return nil, fmt.Errorf("decode intern assignment: %w", err)
		}
		if _, ok := s.plans[assignment.Position]; !ok {
			return nil, fmt.Errorf("unknown saved position %q", assignment.Position)
		}
		s.position = assignment.Position
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read intern assignment: %w", err)
	}

	data, err = os.ReadFile(requestFile)
	if err == nil {
		if err := json.Unmarshal(data, &s.requests); err != nil {
			return nil, fmt.Errorf("decode requests: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read requests: %w", err)
	}
	if err := loadRecords(s.absenceFile, &s.absences); err != nil {
		return nil, fmt.Errorf("load absences: %w", err)
	}
	if err := loadRecords(s.messageFile, &s.messages); err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	return s, nil
}

func main() {
	dataDir := os.Getenv("MOCK_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join("mock-service", "data")
	}
	service, err := newService(
		filepath.Join(dataDir, "request.json"),
		filepath.Join(dataDir, "intern.json"),
	)
	if err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("MOCK_SERVICE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	log.Printf("Сервис планов и заявок запущен на %s", addr)
	log.Fatal(http.ListenAndServe(addr, service.handler()))
}

func (s *Service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /interns/{id}/onboarding-plan", s.getPlan)
	mux.HandleFunc("PUT /demo/interns/{id}/position", s.setPosition)
	mux.HandleFunc("GET /requests/current", s.getRequest)
	mux.HandleFunc("POST /requests", s.createRequest)
	mux.HandleFunc("POST /requests/{id}/approve", s.approveRequest)
	mux.HandleFunc("GET /absences", s.getAbsences)
	mux.HandleFunc("POST /absences", s.createAbsence)
	mux.HandleFunc("POST /absences/{id}/approve", s.approveAbsence)
	mux.HandleFunc("GET /messages", s.getMessages)
	mux.HandleFunc("POST /messages", s.createMessage)
	mux.HandleFunc("POST /mcp", s.handleMCP)
	return mux
}

func (s *Service) getPlan(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != internID {
		writeError(w, http.StatusNotFound, "Стажёр не найден")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.plans[s.position])
}

func (s *Service) setPosition(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != internID {
		writeError(w, http.StatusNotFound, "Стажёр не найден")
		return
	}
	var input struct {
		Position string `json:"position"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON позиции")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.plans[input.Position]; !ok {
		writeError(w, http.StatusBadRequest, "Неподдерживаемая позиция")
		return
	}
	if err := saveJSON(s.internFile, struct {
		Position string `json:"position"`
	}{input.Position}); err != nil {
		log.Println(err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить позицию")
		return
	}
	s.position = input.Position
	writeJSON(w, http.StatusOK, s.plans[s.position])
}

func (s *Service) findStep(planID, stepID string) (Step, bool) {
	for _, plan := range s.plans {
		if plan.PlanID == planID {
			for _, step := range plan.Steps {
				if step.ID == stepID && step.ActionType == "request_access" {
					return step, true
				}
			}
		}
	}
	return Step{}, false
}

func (s *Service) getRequest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("internId") != internID {
		writeError(w, http.StatusNotFound, "Стажёр не найден")
		return
	}
	planID, stepID := query.Get("planId"), query.Get("stepId")
	if _, ok := s.findStep(planID, stepID); !ok {
		writeError(w, http.StatusBadRequest, "Неизвестный шаг запроса доступа")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, request := range s.requests {
		if request.InternID == internID && request.PlanID == planID && request.StepID == stepID {
			writeJSON(w, http.StatusOK, map[string]*Request{"request": request})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]*Request{"request": nil})
}

func (s *Service) createRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != internID {
		writeError(w, http.StatusForbidden, "Заявку может создать только стажёр")
		return
	}
	var input struct {
		PlanID   string `json:"planId"`
		StepID   string `json:"stepId"`
		Resource string `json:"resource"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON заявки")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if input.PlanID != s.plans[s.position].PlanID {
		writeError(w, http.StatusBadRequest, "План не назначен стажёру")
		return
	}
	step, ok := s.findStep(input.PlanID, input.StepID)
	if !ok || input.Resource != step.Resource {
		writeError(w, http.StatusBadRequest, "Неизвестный шаг или ресурс заявки")
		return
	}
	for _, request := range s.requests {
		if request.InternID == internID && request.PlanID == input.PlanID && request.StepID == input.StepID {
			writeJSON(w, http.StatusOK, map[string]*Request{"request": request})
			return
		}
	}

	request := &Request{
		ID:        fmt.Sprintf("DEMO-%04d", 4906+len(s.requests)),
		InternID:  internID,
		PlanID:    input.PlanID,
		StepID:    input.StepID,
		Resource:  step.Resource,
		Status:    "pending",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	updated := append(append([]*Request(nil), s.requests...), request)
	if err := saveJSON(s.requestFile, updated); err != nil {
		log.Println(err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить заявку")
		return
	}
	s.requests = updated
	writeJSON(w, http.StatusCreated, map[string]*Request{"request": request})
}

func (s *Service) approveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != "mentor-1" {
		writeError(w, http.StatusForbidden, "Согласовать заявку может только наставник")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, request := range s.requests {
		if request.ID != r.PathValue("id") {
			continue
		}
		if request.Status == "approved" {
			writeJSON(w, http.StatusOK, map[string]*Request{"request": request})
			return
		}
		approved := *request
		approved.Status = "approved"
		approved.ApprovedAt = time.Now().UTC().Format(time.RFC3339)
		updated := append([]*Request(nil), s.requests...)
		updated[index] = &approved
		if err := saveJSON(s.requestFile, updated); err != nil {
			log.Println(err)
			writeError(w, http.StatusInternalServerError, "Не удалось обновить заявку")
			return
		}
		s.requests = updated
		writeJSON(w, http.StatusOK, map[string]*Request{"request": &approved})
		return
	}
	writeError(w, http.StatusNotFound, "Заявка не найдена")
}

func decodeBody(r *http.Request, value any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected data after JSON body")
	}
	return nil
}

func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
