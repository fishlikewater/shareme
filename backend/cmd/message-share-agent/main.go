package main

import (
	"fmt"

	"message-share/backend/internal/config"
)

func main() {
	cfg := config.Default()
	fmt.Printf("Message Share agent bootstrap on %s\n", cfg.LocalAPIAddr)
}
