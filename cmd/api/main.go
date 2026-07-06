package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/env"
	"github.com/praetordev/praetor/services/api"
)

func main() {
	// Fail fast on a missing/invalid encryption or JWT secret rather than
	// booting with a known-insecure built-in key.
	if err := crypto.ValidateSecrets(true); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	port := env.String("PORT", "8080")

	database, err := db.Connect(env.String("DATABASE_URL", db.DefaultDSN))
	if err != nil {
		log.Printf("Warning: Failed to connect to DB: %v", err)
		log.Println("Starting in NO-DB mode (endpoints will fail)")
	}

	router := api.NewRouter(database, api.Config{
		IngestionURL:   env.String("INGESTION_URL", ""),
		LDAPConfigPath: env.String("PRAETOR_LDAP_CONFIG", ""),
	})

	fmt.Printf("Praetor API Service starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
