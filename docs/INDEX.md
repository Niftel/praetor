# Praetor Documentation Index

This directory contains the core documentation for the Praetor backend.

- [Vision](vision.md) - High-level goals, resilience philosophy, and conceptual architecture.
- [Repository Topology and Ownership](repository-topology.md) - Poly-repo boundaries, development modes, and ownership rules.
- [Extracted Repository Health](repository-health.md) - Standalone build/CI baseline for deployable services.
- [Platform Releases](releasing.md) - Compatibility promotion and artifact preflight procedure.
- [Wire Contracts](wire-contracts.md) - Versioned cross-service payloads and compatibility rules.
- [Execution State Machine](execution-state-machine.md) - Authoritative state ownership and monotonic projection rules.
- [RBAC](RBAC.md) - Persisted grants, the RBAC v4 decision engine, and API enforcement boundaries.
- [Delegated API users](DELEGATED_API_USERS.md) - Bounded service principals for application-triggered workflow launches.
- [Chaos Testing](chaos-testing.md) - Repeatable PostgreSQL-outage and JetStream-restart resilience checks.
- [Architecture](architecture.md) - Kubernetes-native design, data models, and component diagrams.
- [REST API v1](praetor_rest_api_v1_full.md) - Detailed API reference for the Control Plane and Execution Plane.
- [Backend Roadmap](praetor_backend_roadmap_for_agent.md) - Phased implementation plan for the backend agent.
