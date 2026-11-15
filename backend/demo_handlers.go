package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func decodeDemoInput(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Некорректный формат запроса"})
		return false
	}
	return true
}

func validUTCDate(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Format("2006-01-02") == value
}

func writeDemoError(w http.ResponseWriter, err error, noun string) {
	log.Printf("Демо-сервис %s: %v", noun, err)
	var upstream *upstreamError
	if errors.As(err, &upstream) {
		switch upstream.status {
		case http.StatusForbidden:
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Демо-сервис отклонил роль"})
			return
		case http.StatusNotFound:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Запись не найдена"})
			return
		case http.StatusConflict:
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Данные изменились; обнови страницу"})
			return
		}
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Демо-сервис недоступен или вернул некорректный ответ"})
}

func (a *App) getAbsences(w http.ResponseWriter, r *http.Request) {
	if a.absences == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Сервис отгулов не настроен"})
		return
	}
	requests, err := a.absences.List(r.Context(), demoInternID)
	if err != nil {
		writeDemoError(w, err, "отгулов")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Requests []AbsenceRequest `json:"requests"`
	}{requests})
}

func (a *App) createAbsence(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != demoInternID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Отгул может запросить только стажёр"})
		return
	}
	var input struct {
		Date string `json:"date"`
	}
	if !decodeDemoInput(w, r, &input) {
		return
	}
	if !validUTCDate(input.Date) || input.Date < time.Now().UTC().Format("2006-01-02") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Укажи дату отгула в формате YYYY-MM-DD, не раньше сегодня (UTC)"})
		return
	}
	if a.absences == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Сервис отгулов не настроен"})
		return
	}
	request, status, err := a.absences.Create(r.Context(), demoInternID, input.Date)
	if err != nil {
		writeDemoError(w, err, "отгулов")
		return
	}
	writeJSON(w, status, struct {
		Request *AbsenceRequest `json:"request"`
	}{request})
}

func (a *App) approveAbsence(w http.ResponseWriter, r *http.Request) {
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if r.Header.Get("X-Demo-User") != plan.Mentor.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Согласовать отгул может только назначенный наставник"})
		return
	}
	if a.absences == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Сервис отгулов не настроен"})
		return
	}
	id := r.PathValue("id")
	requests, err := a.absences.List(r.Context(), demoInternID)
	if err != nil {
		writeDemoError(w, err, "отгулов")
		return
	}
	found := false
	for _, request := range requests {
		if request.ID == id {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Отгул не найден"})
		return
	}
	request, status, err := a.absences.Approve(r.Context(), plan.Mentor.ID, demoInternID, id)
	if err != nil {
		writeDemoError(w, err, "отгулов")
		return
	}
	writeJSON(w, status, struct {
		Request *AbsenceRequest `json:"request"`
	}{request})
}

func (a *App) getMentorMessages(w http.ResponseWriter, r *http.Request) {
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if a.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Демо-мессенджер не настроен"})
		return
	}
	messages, err := a.messages.List(r.Context(), demoInternID, plan.Mentor.ID, plan.PlanID)
	if err != nil {
		writeDemoError(w, err, "сообщений")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Mentor   Mentor    `json:"mentor"`
		Messages []Message `json:"messages"`
	}{plan.Mentor, messages})
}

func (a *App) createMentorMessage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != demoInternID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Написать наставнику может только стажёр"})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if !decodeDemoInput(w, r, &input) {
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" || utf8.RuneCountInString(input.Text) > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Сообщение должно содержать от 1 до 500 символов"})
		return
	}
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if a.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Демо-мессенджер не настроен"})
		return
	}
	message, status, err := a.messages.Send(r.Context(), demoInternID, plan.Mentor.ID, plan.PlanID, input.Text)
	if err != nil {
		writeDemoError(w, err, "сообщений")
		return
	}
	writeJSON(w, status, struct {
		Message *Message `json:"message"`
	}{message})
}
