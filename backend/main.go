package main

import (
	"log"
	"net/http"
	"path/filepath"
)

func main() {
	app, err := newApp(filepath.Join("backend", "data", "request.json"))
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", newHandler(app)))
}

func newHandler(app *App) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/plan", app.getPlan)
	mux.HandleFunc("GET /api/requests/current", app.getRequest)
	mux.HandleFunc("POST /api/requests", app.createRequest)
	mux.HandleFunc("POST /api/requests/{id}/approve", app.approveRequest)

	return allowFrontend(mux)
}

func allowFrontend(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")

		if origin != "" && origin != "http://localhost:5173" {
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
