package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (a *App) answerBuddy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Question string `json:"question"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Передай вопрос в формате JSON"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Некорректный формат вопроса"})
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" || utf8.RuneCountInString(input.Question) > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Вопрос должен содержать от 1 до 500 символов"})
		return
	}
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if asksMentorIdentity(input.Question) {
		writeJSON(w, http.StatusOK, mentorIdentityAnswer(plan))
		return
	}
	if a.assistant == nil || a.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Помощник не настроен"})
		return
	}
	if asksRequestStatus(input.Question) && a.statusTool != nil {
		for _, step := range plan.Steps {
			if step.ActionType != "request_access" {
				continue
			}
			status, err := a.statusTool.GetStatus(r.Context(), plan, step)
			if err != nil {
				log.Printf("MCP status tool failed: %v", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Не удалось проверить статус заявки. Попробуй позже."})
				return
			}
			// A new assignment or approval can race this read; refuse inconsistent snapshots.
			if status.Status == "not_created" && plan.Request != nil ||
				status.Status != "not_created" && (plan.Request == nil || plan.Request.ID != status.RequestID || plan.Request.Status != status.Status) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "Статус изменился. Обнови план и повтори вопрос."})
				return
			}
			writeJSON(w, http.StatusOK, statusAnswer(plan, step, status))
			return
		}
	}
	answer, err := a.assistant.Answer(r.Context(), plan, input.Question, a.catalog)
	if err != nil {
		switch {
		case errors.Is(err, ErrAIUnconfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Модель не настроена. Укажи AI_API_KEY в .env и перезапусти Go-сервер.",
			})
		case errors.Is(err, ErrAIRateLimited):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Лимит запросов к модели исчерпан. Попробуй позже.",
			})
		default:
			log.Printf("Не удалось получить ответ AI: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "Модель недоступна. Попробуй позже.",
			})
		}
		return
	}
	writeJSON(w, http.StatusOK, answer)
}
