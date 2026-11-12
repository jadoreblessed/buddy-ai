package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Request struct {
	ID         string `json:"id"`
	InternID   string `json:"internId"`
	Resource   string `json:"resource"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type Service struct {
	mu      sync.Mutex
	request *Request
	file    string
}

func main() {
	service := &Service{
		file: filepath.Join("mock-service", "data", "request.json"),
	}

	data, err := os.ReadFile(service.file)
	if err == nil {
		if err := json.Unmarshal(data, &service.request); err != nil {
			log.Fatal(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /requests/current", service.getRequest)
	mux.HandleFunc("POST /requests", service.createRequest)
	mux.HandleFunc("POST /requests/{id}/approve", service.approveRequest)

	log.Println("Сервис заявок запущен на http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}

func (s *Service) getRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]*Request{"request": s.request})
}

func (s *Service) createRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != "intern-1" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Заявку может создать только стажёр",
		})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.request != nil {
		writeJSON(w, http.StatusOK, map[string]*Request{"request": s.request})
		return
	}

	request := &Request{
		ID:        "DEMO-4906",
		InternID:  "intern-1",
		Resource:  "Тестовое окружение",
		Status:    "pending",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.save(request); err != nil {
		log.Println(err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Не удалось сохранить заявку",
		})
		return
	}

	s.request = request
	writeJSON(w, http.StatusCreated, map[string]*Request{"request": request})
}

func (s *Service) approveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != "mentor-1" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Согласовать заявку может только наставник",
		})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.request == nil || s.request.ID != r.PathValue("id") {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Заявка не найдена",
		})
		return
	}

	if s.request.Status == "approved" {
		writeJSON(w, http.StatusOK, map[string]*Request{"request": s.request})
		return
	}

	updated := *s.request
	updated.Status = "approved"
	updated.ApprovedAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.save(&updated); err != nil {
		log.Println(err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Не удалось обновить заявку",
		})
		return
	}

	s.request = &updated
	writeJSON(w, http.StatusOK, map[string]*Request{"request": s.request})
}

func (s *Service) save(request *Request) error {
	if err := os.MkdirAll(filepath.Dir(s.file), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.file, data, 0600)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
