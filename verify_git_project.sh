#!/bin/bash
API_URL="http://localhost:8080/api/v1"

# Cleanup previous runs
echo "Cleaning up..."
docker exec praetor-db-1 psql -U postgres -d praetor -c "DELETE FROM job_templates WHERE name = 'LAMP Stack Git';" > /dev/null 2>&1
docker exec praetor-db-1 psql -U postgres -d praetor -c "DELETE FROM projects WHERE name = 'Ansible Examples';" > /dev/null 2>&1
docker exec praetor-db-1 psql -U postgres -d praetor -c "DELETE FROM inventories WHERE name = 'LAMP Git Inventory';" > /dev/null 2>&1

echo "1. Creating Inventory..."
INV_RES=$(curl -s -v -X POST "$API_URL/inventories" \
  -H "Content-Type: application/json" \
  -d '{
    "organization_id": 1,
    "name": "LAMP Git Inventory",
    "content": "[web]\nweb1 ansible_host=praetor-web1-1 ansible_user=root ansible_password=password\n\n[db]\ndb1 ansible_host=praetor-db1-1 ansible_user=root ansible_password=password"
  }')
echo "INV_RES: $INV_RES"
INV_ID=$(echo "$INV_RES" | grep "{" | jq -r '.id')
echo "Inventory ID: $INV_ID"

if [ -z "$INV_ID" ] || [ "$INV_ID" == "null" ]; then
  echo "Failed to create Inventory"
  exit 1
fi

echo "2. Creating Project..."
PROJ_RES=$(curl -s -v -X POST "$API_URL/projects" \
  -H "Content-Type: application/json" \
  -d '{
    "organization_id": 1,
    "name": "Ansible Examples",
    "scm_type": "git",
    "scm_url": "https://github.com/ansible/ansible-examples.git",
    "scm_branch": "master"
  }')
echo "PROJ_RES: $PROJ_RES"
PROJ_ID=$(echo "$PROJ_RES" | grep "{" | jq -r '.id')
echo "Project ID: $PROJ_ID"

if [ -z "$PROJ_ID" ] || [ "$PROJ_ID" == "null" ]; then
  echo "Failed to create Project"
  exit 1
fi

echo "3. Syncing Project..."
curl -s -v -X POST "$API_URL/projects/$PROJ_ID/sync"

echo "4. Creating Template..."
# Construct JSON properly
TEMP_JSON=$(cat <<EOF
{
  "organization_id": 1,
  "name": "LAMP Stack Git",
  "project_id": $PROJ_ID,
  "inventory_id": $INV_ID,
  "playbook": "lamp_simple/site.yml",
  "job_type": "run"
}
EOF
)

TEMP_RES=$(curl -s -v -X POST "$API_URL/job-templates" \
  -H "Content-Type: application/json" \
  -d "$TEMP_JSON")
echo "TEMP_RES: $TEMP_RES"
TEMPLATE_ID=$(echo "$TEMP_RES" | grep "{" | jq -r '.unified_job_template_id')
echo "Template ID: $TEMPLATE_ID"

if [ -z "$TEMPLATE_ID" ] || [ "$TEMPLATE_ID" == "null" ]; then
  echo "Failed to create Template"
  exit 1
fi

echo "5. Launching Job..."
LAUNCH_RES=$(curl -s -v -X POST "$API_URL/job-templates/$TEMPLATE_ID/launch" \
  -H "Content-Type: application/json" \
  -d '{}')
echo "LAUNCH_RES: $LAUNCH_RES"
JOB_ID=$(echo "$LAUNCH_RES" | grep "{" | jq -r '.id')
echo "Launched Job: $JOB_ID"

if [ -z "$JOB_ID" ] || [ "$JOB_ID" == "null" ]; then
  echo "Failed to launch Job"
  exit 1
fi

echo "6. Waiting for completion..."
sleep 5
RUN_ID=$(curl -s "$API_URL/jobs/$JOB_ID" | jq -r '.current_run_id')
echo "Run ID: $RUN_ID"

for i in {1..60}; do
  STATE=$(curl -s "$API_URL/jobs/runs/$RUN_ID" | jq -r '.state')
  echo "Job State: $STATE"
  if [ "$STATE" == "successful" ]; then
    echo "SUCCESS: Job completed successfully."
    exit 0
  fi
  if [ "$STATE" == "failed" ]; then
    echo "FAILURE: Job failed."
    # Dump events to see why
    curl -s "$API_URL/jobs/runs/$RUN_ID/events"
    exit 1
  fi
  sleep 5
done

echo "TIMEOUT: Job did not complete in time."
exit 1
