package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	secretsclient "github.com/Niftel/praetor-secrets/client"
	"github.com/praetordev/crypto"
	"github.com/praetordev/db"
	"github.com/praetordev/env"
	"github.com/praetordev/plog"
	"github.com/praetordev/praetor/services/api"
	modelAuth "github.com/praetordev/praetor/services/api/middleware"
)

func main() {
	plog.Configure("api") // structured logging seam; stdlib log routes through it
	// Fail fast on a missing/invalid encryption or JWT secret rather than
	// booting with a known-insecure built-in key.
	if err := crypto.ValidateSecrets(true); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	port := env.String("PORT", "8080")

	database, err := db.Connect(env.String("DATABASE_URL", db.DefaultDSN))
	if err != nil {
		log.Fatalf("database unavailable: %v", err)
	}
	defer database.Close()

	refreshInterval, err := time.ParseDuration(env.String("PRAETOR_RBAC_POLICY_REFRESH_INTERVAL", "30s"))
	if err != nil {
		log.Fatalf("invalid PRAETOR_RBAC_POLICY_REFRESH_INTERVAL: %v", err)
	}
	auditDecisions, err := strconv.ParseBool(env.String("PRAETOR_RBAC_DECISION_AUDIT", "false"))
	if err != nil {
		log.Fatalf("invalid PRAETOR_RBAC_DECISION_AUDIT: %v", err)
	}
	credentialSecrets, err := newCredentialSecretsClient()
	if err != nil {
		log.Fatalf("secrets service misconfigured: %v", err)
	}
	activityTimeout, err := time.ParseDuration(env.String("PRAETOR_ACTIVITY_WRITE_TIMEOUT", "2s"))
	if err != nil {
		log.Fatalf("invalid PRAETOR_ACTIVITY_WRITE_TIMEOUT: %v", err)
	}
	if activityTimeout <= 0 {
		log.Fatal("PRAETOR_ACTIVITY_WRITE_TIMEOUT must be positive")
	}
	activityRecorder := modelAuth.NewActivityRecorder(context.Background(), database, activityTimeout)
	router, err := api.NewRouter(database, api.Config{
		CredentialSecrets:         credentialSecrets,
		IngestionURL:              env.String("INGESTION_URL", ""),
		InternalToken:             env.String("PRAETOR_INTERNAL_TOKEN", ""),
		LDAPConfigPath:            env.String("PRAETOR_LDAP_CONFIG", ""),
		RBACPolicyPath:            env.String("PRAETOR_RBAC_POLICY", ""),
		RBACPolicySHA256:          env.String("PRAETOR_RBAC_POLICY_SHA256", ""),
		RBACPolicyRefreshInterval: refreshInterval,
		RBACDecisionAudit:         auditDecisions,
		ActivityRecorder:          activityRecorder,
	})
	if err != nil {
		log.Fatalf("API configuration invalid: %v", err)
	}

	fmt.Printf("Praetor API Service starting on port %s...\n", port)
	server := &http.Server{Addr: ":" + port, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	serviceCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() { serveError <- server.ListenAndServe() }()

	var serverFailure error
	select {
	case <-serviceCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("API shutdown failed: %v", err)
		}
		cancel()
	case err := <-serveError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverFailure = err
		}
	}

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), activityTimeout+time.Second)
	if err := activityRecorder.Close(drainCtx); err != nil {
		log.Printf("activity audit drain failed: %v", err)
	}
	cancelDrain()
	if serverFailure != nil {
		log.Fatalf("API server failed: %v", serverFailure)
	}
}

// newCredentialSecretsClient deliberately accepts the workload private key
// only as a file path. Setting the URL opts into the integration and makes every
// mTLS setting mandatory; partial configuration fails startup.
func newCredentialSecretsClient() (*secretsclient.Client, error) {
	baseURL := env.String("PRAETOR_SECRETS_URL", "")
	if baseURL == "" {
		return nil, nil
	}
	timeout, err := time.ParseDuration(env.String("PRAETOR_SECRETS_TIMEOUT", "10s"))
	if err != nil {
		return nil, err
	}
	return secretsclient.New(secretsclient.Config{
		BaseURL:         baseURL,
		CAFile:          env.String("PRAETOR_SECRETS_CA_FILE", ""),
		CertificateFile: env.String("PRAETOR_SECRETS_CERT_FILE", ""),
		PrivateKeyFile:  env.String("PRAETOR_SECRETS_KEY_FILE", ""),
		Timeout:         timeout,
	})
}
