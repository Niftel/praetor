# Praetor Quick Start Guide

Follow these steps to deploy Praetor on a new machine with Docker installed.

## Prerequisites
- **Git**
- **Docker** & **Docker Compose**
- **Make** (Optional, but recommended)

## 1. Clone the Repository
```bash
git clone https://github.com/Niftel/praetor.git
cd praetor
```

## 2. Start the Stack (Automated)
The `Makefile` handles SSH key generation and container startup automatically.

```bash
make up
```

*If you don't have `make` installed, you can manually generate keys and run `docker compose up`.*

## 4. Access the Application
- **Web UI**: [http://localhost:3000](http://localhost:3000)
- **API**: [http://localhost:8080](http://localhost:8080)
- **Target Web Servers**: 
  - [http://localhost:8081](http://localhost:8081)
  - [http://localhost:8082](http://localhost:8082)

## 5. Initial Setup
1. Go to the **Projects** page and click "Sync" on the default project.
2. Go to **Templates** and launch the "Test Connection" or "LAMP Stack" job.
