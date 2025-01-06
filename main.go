package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	mu         sync.Mutex
	data       map[string]string
	requests   int
	shutdownCh chan struct{}
}

func NewServer() *Server {
	return &Server{
		data:       make(map[string]string),
		shutdownCh: make(chan struct{}),
	}
}

func (s *Server) postDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	for key, value := range payload {
		s.data[key] = value
	}
	s.requests++
	s.mu.Unlock()

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) getDataHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.data)
	s.requests++
}

func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := map[string]int{
		"requests":      s.requests,
		"database_size": len(s.data),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
	s.requests++
}

func (s *Server) deleteDataHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path[len("/data/"):]

	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.data[key]; !exists {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}
	delete(s.data, key)
	s.requests++
	w.WriteHeader(http.StatusOK)
}

func (s *Server) startBackgroundWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			log.Printf("Server status: requests=%d, database_size=%d", s.requests, len(s.data))
			s.mu.Unlock()
		case <-s.shutdownCh:
			log.Println("Background worker stopping...")
			return
		}
	}
}

func (s *Server) shutdown() {
	log.Println("Shutting down server...")
	close(s.shutdownCh)
}

func main() {
	server := NewServer()

	// Регистрация обработчиков
	http.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			server.postDataHandler(w, r)
		case http.MethodGet:
			server.getDataHandler(w, r)
		default:
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/data/", server.deleteDataHandler) // Удаление по ключу
	http.HandleFunc("/stats", server.statsHandler)

	go server.startBackgroundWorker()

	srv := &http.Server{Addr: ":8080"}

	go func() {
		log.Println("Server starting on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe error: %v", err)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	<-signalCh

	server.shutdown()

	srv.Close()
	log.Println("Server gracefully stopped.")
}
