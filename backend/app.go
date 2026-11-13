package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

const demoInternID = "intern-1"

type Step struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	ActionType string `json:"actionType"`
	Resource   string `json:"resource,omitempty"`
	Status     string `json:"status"`
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

type Plan struct {
	PlanID   string   `json:"planId"`
	Intern   string   `json:"intern"`
	Position string   `json:"position"`
	Role     string   `json:"role"`
	Team     string   `json:"team"`
	Progress int      `json:"progress"`
	Steps    []Step   `json:"steps"`
	Request  *Request `json:"request"`
}

type App struct {
	source  OnboardingSource
	tickets *TicketClient
}

func (a *App) getPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (a *App) getRequest(w http.ResponseWriter, r *http.Request) {
	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]*Request{"request": plan.Request})
}

func (a *App) createRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != demoInternID {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Заявку может создать только стажёр",
		})
		return
	}

	plan, err := a.GetAssignedPlan(r.Context(), demoInternID)
	if err != nil {
		writePlanError(w, err)
		return
	}
	if plan.Request != nil {
		writeJSON(w, http.StatusOK, map[string]*Request{"request": plan.Request})
		return
	}
	for _, step := range plan.Steps {
		if step.Status == "current" && step.ActionType == "request_access" {
			request, status, err := a.tickets.Create(r.Context(), demoInternID,
				plan.PlanID, step.ID, step.Resource)
			if err != nil {
				writeTicketError(w, status, err)
				return
			}
			writeJSON(w, status, map[string]*Request{"request": request})
			return
		}
	}
	writeJSON(w, http.StatusConflict, map[string]string{
		"error": "На текущем шаге заявка на доступ не требуется",
	})
}

func (a *App) approveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != "mentor-1" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Согласовать заявку может только наставник",
		})
		return
	}
	request, status, err := a.tickets.Approve(r.Context(), "mentor-1", r.PathValue("id"))
	if err != nil {
		writeTicketError(w, status, err)
		return
	}
	writeJSON(w, status, map[string]*Request{"request": request})
}

func writePlanError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrInternNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Стажёр не найден"})
		return
	}
	log.Printf("План адаптации: %v", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"error": "Не удалось загрузить назначенный план или статус заявки",
	})
}

func writeTicketError(w http.ResponseWriter, status int, err error) {
	log.Printf("Сервис заявок: %v", err)
	if status == http.StatusNotFound {
		writeJSON(w, status, map[string]string{"error": "Заявка не найдена"})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"error": "Не удалось выполнить действие через сервис заявок",
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
