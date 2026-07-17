package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	secretsclient "github.com/Niftel/praetor-secrets/client"
	"github.com/praetordev/crypto"
	"github.com/praetordev/db"
	"github.com/praetordev/env"
	"github.com/praetordev/plog"
	"github.com/praetordev/praetor/services/api"
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
	router := api.NewRouter(database, api.Config{
		CredentialSecrets:         credentialSecrets,
		IngestionURL:              env.String("INGESTION_URL", ""),
		InternalToken:             env.String("PRAETOR_INTERNAL_TOKEN", ""),
		LDAPConfigPath:            env.String("PRAETOR_LDAP_CONFIG", ""),
		RBACPolicyPath:            env.String("PRAETOR_RBAC_POLICY", ""),
		RBACPolicySHA256:          env.String("PRAETOR_RBAC_POLICY_SHA256", ""),
		RBACPolicyRefreshInterval: refreshInterval,
		RBACDecisionAudit:         auditDecisions,
	})

	fmt.Printf("Praetor API Service starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("Failed to start server: %v", err)
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
