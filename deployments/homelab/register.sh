#!/usr/bin/env bash
# Register the RHEL 9 (Rocky) home lab into Praetor: an inventory "homelab" with
# rocky1/2/3, rocky1 as the runner/control node, plus a smoke-test job template.
# Idempotent-ish: re-running creates duplicates, so it's meant as a one-shot / doc.
#
# Prereqs: `up.sh` has started the nodes; Praetor is reachable at $BASE; you have an
# admin bearer token in $TOKEN. Reuses the org-1 sandbox-machine credential (id 4,
# user root) — its public key is what up.sh authorizes on the nodes.
set -euo pipefail

BASE="${BASE:-https://praetor.localhost/api/v1}"
TOKEN="${TOKEN:?set TOKEN to an admin bearer token}"
CRED_ID="${CRED_ID:-4}"      # sandbox-machine (root, org 1)
PROJECT_ID="${PROJECT_ID:-9}" # sandbox-play (hosts: all smoke test), org 1
PACK_ID="${PACK_ID:-1}"       # ansible-runtime
ORG_ID="${ORG_ID:-1}"

# curl helper that forces praetor.localhost -> 127.0.0.1 (k3d ingress) and auths.
C() { curl -sk --resolve praetor.localhost:443:127.0.0.1 \
        -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }
id() { python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))'; }
ujt() { python3 -c 'import sys,json;print(json.load(sys.stdin).get("unified_job_template_id",""))'; }

INV=$(C -X POST "$BASE/inventories" \
  -d "{\"name\":\"homelab\",\"organization_id\":$ORG_ID,\"description\":\"RHEL 9 (Rocky) home lab: rocky1 is the runner/control node\"}" | id)
echo "inventory homelab=$INV"

# rocky1 is the runner: Praetor SSHes to it at host.k3d.internal:2201 and its
# bundled Ansible manages the fleet. ansible_connection=local so tasks targeting
# rocky1 run on it directly. rocky2/rocky3 are reached from rocky1 by container name.
R1=$(C -X POST "$BASE/inventories/$INV/hosts" -d '{"name":"rocky1","variables":{"ansible_host":"host.k3d.internal","ansible_port":2201,"ansible_connection":"local"}}' | id)
C -X POST "$BASE/inventories/$INV/hosts" -d '{"name":"rocky2","variables":{"ansible_host":"rocky2","ansible_port":22}}' >/dev/null
C -X POST "$BASE/inventories/$INV/hosts" -d '{"name":"rocky3","variables":{"ansible_host":"rocky3","ansible_port":22}}' >/dev/null
C -X POST "$BASE/hosts/$R1/set-runner" >/dev/null
echo "hosts created; rocky1 ($R1) is the runner"

UJT=$(C -X POST "$BASE/job-templates" \
  -d "{\"name\":\"homelab-smoke\",\"organization_id\":$ORG_ID,\"inventory_id\":$INV,\"project_id\":$PROJECT_ID,\"playbook\":\"site.yml\",\"credential_id\":$CRED_ID,\"execution_pack_id\":$PACK_ID,\"forks\":5}" | ujt)
echo "job template homelab-smoke ujt=$UJT"
echo
echo "Launch it with:"
echo "  curl -sk --resolve praetor.localhost:443:127.0.0.1 -H \"Authorization: Bearer \$TOKEN\" \\"
echo "    -H 'Content-Type: application/json' -X POST $BASE/jobs \\"
echo "    -d '{\"unified_job_template_id\":$UJT,\"name\":\"homelab-smoke run\"}'"
