package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) callService(ctx context.Context, method, path, user string) (*Request, int, error) {
	url := strings.TrimRight(a.serviceURL, "/") + path

	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if user != "" {
		request.Header.Set("X-Demo-User", user)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("сервис вернул HTTP %d", response.StatusCode)
	}

	var result struct {
		Request *Request `json:"request"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, response.StatusCode, err
	}

	return result.Request, response.StatusCode, nil
}

func (a *App) syncRequest(ctx context.Context) error {
	request, _, err := a.callService(ctx, http.MethodGet, "/requests/current", "")
	if err != nil {
		log.Printf("Сервис заявок: %v", err)
		return err
	}

	a.mu.Lock()
	a.request = request
	a.mu.Unlock()
	return nil
}

func (a *App) forwardAction(w http.ResponseWriter, r *http.Request, path, user string) {
	request, status, err := a.callService(r.Context(), http.MethodPost, path, user)
	if err != nil {
		log.Printf("Сервис заявок: %v", err)

		if status == http.StatusNotFound {
			writeJSON(w, status, map[string]string{"error": "Заявка не найдена"})
			return
		}

		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Не удалось выполнить действие через сервис заявок",
		})
		return
	}

	a.mu.Lock()
	a.request = request
	a.mu.Unlock()

	writeJSON(w, status, map[string]*Request{"request": request})
}
