package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/database"
	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	email, err := identity.NormalizeEmail(os.Getenv("BOOTSTRAP_OWNER_EMAIL"))
	password := os.Getenv("BOOTSTRAP_OWNER_PASSWORD")
	if databaseURL == "" || err != nil || password == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL, BOOTSTRAP_OWNER_EMAIL and BOOTSTRAP_OWNER_PASSWORD are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	passwordHash, err := identity.HashPasswordContext(ctx, password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "owner password preparation failed; check password length (12 to 128 bytes) and retry")
		os.Exit(2)
	}
	store, err := database.Open(ctx, databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database connection configuration is invalid")
		os.Exit(1)
	}
	defer store.Close()
	userID, err := store.BootstrapOwner(ctx, email, passwordHash, time.Now().UTC())
	if errors.Is(err, operations.ErrConflict) {
		fmt.Fprintln(os.Stderr, "an active owner with a different email already exists")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "owner bootstrap failed")
		os.Exit(1)
	}
	fmt.Printf("owner ready: %s\n", userID)
}
