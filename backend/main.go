package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	ticketURL := os.Getenv("TICKET_SERVICE_URL")
	if ticketURL == "" {
		ticketURL = "http://localhost:8081"
	}
	onboardingURL := os.Getenv("ONBOARDING_SERVICE_URL")
	if onboardingURL == "" {
		onboardingURL = ticketURL
	}
	aiConfig, err := loadAIConfig(".env")
	if err != nil {
		log.Fatal(err)
	}
	catalog, err := loadDefaultMaterialCatalog()
	if err != nil {
		log.Fatal(err)
	}
	app := &App{
		source:    NewHTTPOnboardingSource(onboardingURL),
		tickets:   NewTicketClient(ticketURL),
		catalog:   catalog,
		assistant: &Assistant{model: NewOpenAIClient(aiConfig.BaseURL, aiConfig.Model, aiConfig.Key), modelName: aiConfig.Model},
	}
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("Сервер запущен на %s", addr)
	log.Fatal(http.ListenAndServe(addr, newHandler(app)))
}

func newHandler(app *App) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/plan", app.getPlan)
	mux.HandleFunc("GET /api/materials", app.getMaterials)
	mux.HandleFunc("POST /api/buddy/answer", app.answerBuddy)
	mux.HandleFunc("GET /api/requests/current", app.getRequest)
	mux.HandleFunc("POST /api/requests", app.createRequest)
	mux.HandleFunc("POST /api/requests/{id}/approve", app.approveRequest)

	return allowFrontend(mux)
}

func allowFrontend(next http.Handler) http.Handler {
	allowedOrigin := os.Getenv("FRONTEND_ORIGIN")
	alternateOrigin := ""
	if allowedOrigin == "" {
		allowedOrigin = "http://localhost:5173"
		alternateOrigin = "http://127.0.0.1:5173"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")

		if origin != "" && origin != allowedOrigin && origin != alternateOrigin {
			http.Error(w, "Недопустимый адрес сайта", http.StatusForbidden)
			return
		}

		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Demo-User")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
