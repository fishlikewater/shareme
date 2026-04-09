package main

import (
	"log"
	"net/http"

	"message-share/backend/internal/api"
	"message-share/backend/internal/config"
)

func main() {
	cfg := config.Default()
	server := api.NewHTTPServer(api.StubAppService())
	log.Printf("Message Share agent bootstrap on %s", cfg.LocalAPIAddr)
	log.Fatal(http.ListenAndServe(cfg.LocalAPIAddr, server.Handler()))
}
