package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type AbsenceRequest struct {
	ID         string `json:"id"`
	InternID   string `json:"internId"`
	Date       string `json:"date"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type Message struct {
	ID        string `json:"id"`
	InternID  string `json:"internId"`
	PlanID    string `json:"planId"`
	MentorID  string `json:"mentorId"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

func loadRecords(path string, target any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("decode %s: expected array", path)
	}
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("decode %s: trailing content: %v", path, err)
	}
	return nil
}

func (s *Service) getAbsences(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != internID || r.URL.Query().Get("internId") != internID {
		writeError(w, http.StatusForbidden, "Доступно только стажёру")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := make([]*AbsenceRequest, 0, len(s.absences))
	for _, request := range s.absences {
		if request.InternID == internID {
			requests = append(requests, request)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (s *Service) createAbsence(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != internID {
		writeError(w, http.StatusForbidden, "Заявку может создать только стажёр")
		return
	}
	var input struct {
		Date string `json:"date"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON заявки")
		return
	}
	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil || date.Format("2006-01-02") != input.Date || date.Before(time.Now().UTC().Truncate(24*time.Hour)) {
		writeError(w, http.StatusBadRequest, "Дата должна быть сегодняшней или будущей датой UTC в формате YYYY-MM-DD")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, request := range s.absences {
		if request.InternID == internID && request.Date == input.Date {
			writeJSON(w, http.StatusOK, map[string]any{"request": request})
			return
		}
	}
	request := &AbsenceRequest{ID: fmt.Sprintf("ABS-%04d", len(s.absences)+1), InternID: internID, Date: input.Date, Status: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	updated := append(append([]*AbsenceRequest(nil), s.absences...), request)
	if err := saveJSON(s.absenceFile, updated); err != nil {
		log.Println(err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить заявку")
		return
	}
	s.absences = updated
	writeJSON(w, http.StatusCreated, map[string]any{"request": request})
}

func (s *Service) approveAbsence(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != s.currentMentorID() {
		writeError(w, http.StatusForbidden, "Согласовать заявку может только наставник")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, request := range s.absences {
		if request.ID != r.PathValue("id") {
			continue
		}
		if request.Status == "approved" {
			writeJSON(w, http.StatusOK, map[string]any{"request": request})
			return
		}
		approved := *request
		approved.Status = "approved"
		approved.ApprovedAt = time.Now().UTC().Format(time.RFC3339)
		updated := append([]*AbsenceRequest(nil), s.absences...)
		updated[index] = &approved
		if err := saveJSON(s.absenceFile, updated); err != nil {
			log.Println(err)
			writeError(w, http.StatusInternalServerError, "Не удалось согласовать заявку")
			return
		}
		s.absences = updated
		writeJSON(w, http.StatusOK, map[string]any{"request": &approved})
		return
	}
	writeError(w, http.StatusNotFound, "Заявка не найдена")
}

func (s *Service) currentMentorID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plans[s.position].Mentor.ID
}

func (s *Service) getMessages(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != internID || r.URL.Query().Get("internId") != internID {
		writeError(w, http.StatusForbidden, "Доступно только стажёру")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.plans[s.position]
	if r.URL.Query().Get("mentorId") != plan.Mentor.ID || r.URL.Query().Get("planId") != plan.PlanID {
		writeError(w, http.StatusBadRequest, "План или наставник не назначен стажёру")
		return
	}
	messages := make([]*Message, 0, len(s.messages))
	for _, message := range s.messages {
		if message.InternID == internID && message.MentorID == plan.Mentor.ID && message.PlanID == plan.PlanID {
			messages = append(messages, message)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (s *Service) createMessage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != internID {
		writeError(w, http.StatusForbidden, "Написать может только стажёр")
		return
	}
	var input struct {
		PlanID   string `json:"planId"`
		MentorID string `json:"mentorId"`
		Text     string `json:"text"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON сообщения")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" || utf8.RuneCountInString(text) > 500 {
		writeError(w, http.StatusBadRequest, "Сообщение должно содержать от 1 до 500 символов")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.plans[s.position]
	if input.PlanID != plan.PlanID || input.MentorID != plan.Mentor.ID {
		writeError(w, http.StatusBadRequest, "План или наставник не назначен стажёру")
		return
	}
	message := &Message{ID: fmt.Sprintf("MSG-%04d", len(s.messages)+1), InternID: internID, PlanID: input.PlanID, MentorID: input.MentorID, Text: text, Status: "sent", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	updated := append(append([]*Message(nil), s.messages...), message)
	if err := saveJSON(s.messageFile, updated); err != nil {
		log.Println(err)
		writeError(w, http.StatusInternalServerError, "Не удалось отправить сообщение")
		return
	}
	s.messages = updated
	writeJSON(w, http.StatusCreated, map[string]any{"message": message})
}
