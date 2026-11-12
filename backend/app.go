package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type Step struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status string `json:"status"`
}

type Request struct {
	ID         string `json:"id"`
	InternID   string `json:"internId"`
	Resource   string `json:"resource"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

type Plan struct {
	Intern   string   `json:"intern"`
	Role     string   `json:"role"`
	Team     string   `json:"team"`
	Progress int      `json:"progress"`
	Steps    []Step   `json:"steps"`
	Request  *Request `json:"request"`
}

type App struct {
	mu         sync.Mutex
	request    *Request
	file       string
	serviceURL string
}

func (a *App) getPlan(w http.ResponseWriter, r *http.Request) {
	if a.serviceURL != "" {
		if err := a.syncRequest(r.Context()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "Сервис заявок недоступен",
			})
			return
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	accessStatus := "current"
	accessDetail := "Создай заявку на доступ к тестовому окружению."
	nextStatus := "locked"
	progress := 2

	if a.request != nil {
		accessStatus = "pending"
		accessDetail = "Заявка ожидает согласования."

		if a.request.Status == "approved" {
			accessStatus = "done"
			accessDetail = "Доступ согласован."
			nextStatus = "current"
			progress = 3
		}
	}

	plan := Plan{
		Intern:   "Маша Ковалёва",
		Role:     "Backend-стажёр",
		Team:     "Платформа заказов",
		Progress: progress,
		Steps: []Step{
			{1, "Познакомиться с командой", "Встреча с наставником завершена.", "done"},
			{2, "Настроить инструменты", "Рабочая среда настроена.", "done"},
			{3, "Получить доступ к тестовому окружению", accessDetail, accessStatus},
			{4, "Начать первую задачу", "Следующий шаг после получения доступа.", nextStatus},
		},
		Request: a.request,
	}

	writeJSON(w, http.StatusOK, plan)
}

func (a *App) getRequest(w http.ResponseWriter, r *http.Request) {
	if a.serviceURL != "" {
		if err := a.syncRequest(r.Context()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "Сервис заявок недоступен",
			})
			return
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]*Request{
		"request": a.request,
	})
}

func (a *App) createRequest(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-Demo-User")
	if user == "" {
		user = "intern-1"
	}
	if user != "intern-1" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Заявку может создать только стажёр",
		})
		return
	}

	if a.serviceURL != "" {
		a.forwardAction(w, r, "/requests", user)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.request != nil {
		writeJSON(w, http.StatusOK, map[string]*Request{
			"request": a.request,
		})
		return
	}

	request := &Request{
		ID:        "DEMO-4906",
		InternID:  "intern-1",
		Resource:  "Тестовое окружение",
		Status:    "pending",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := a.save(request); err != nil {
		log.Println("Не удалось сохранить заявку:", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Не удалось сохранить заявку",
		})
		return
	}

	a.request = request
	writeJSON(w, http.StatusCreated, map[string]*Request{
		"request": request,
	})
}

func (a *App) approveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Demo-User") != "mentor-1" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Согласовать заявку может только наставник",
		})
		return
	}

	if a.serviceURL != "" {
		a.forwardAction(
			w,
			r,
			"/requests/"+r.PathValue("id")+"/approve",
			"mentor-1",
		)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.request == nil || a.request.ID != r.PathValue("id") {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Заявка не найдена",
		})
		return
	}

	if a.request.Status == "approved" {
		writeJSON(w, http.StatusOK, map[string]*Request{
			"request": a.request,
		})
		return
	}

	updated := *a.request
	updated.Status = "approved"
	updated.ApprovedAt = time.Now().UTC().Format(time.RFC3339)

	if err := a.save(&updated); err != nil {
		log.Println("Не удалось обновить заявку:", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Не удалось обновить заявку",
		})
		return
	}

	a.request = &updated
	writeJSON(w, http.StatusOK, map[string]*Request{
		"request": a.request,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
