#!/usr/bin/env bash
# Demonstrates checkpoint + resume against a real ansible-playbook.
#
#   1. Run the play, "reboot" by killing ansible while task 2 runs.
#   2. Resume WITHOUT restored vars  -> task 3 fails (registered value lost).
#   3. Resume WITH restored vars     -> task 3 succeeds (value restored).
#
# Run inside a host that has ansible-core. Expects play.yml + the callback
# plugin to be present at the paths below.
set -uo pipefail
cd "$(dirname "$0")"

export ANSIBLE_CALLBACK_PLUGINS="$(pwd)/../plugins/callback"
export ANSIBLE_CALLBACKS_ENABLED=praetor_checkpoint
export PRAETOR_CHECKPOINT="$(pwd)/checkpoint.json"
OUT="$(pwd)/outfile.txt"
rm -f "$PRAETOR_CHECKPOINT" "$OUT" restored.json

echo "=== 1. run + simulated reboot (kill during task 2) ==="
ansible-playbook play.yml -e outfile="$OUT" >/dev/null 2>&1 &
APID=$!
for _ in $(seq 1 60); do
  grep -q '"resume_at": "slow task interrupted by reboot"' "$PRAETOR_CHECKPOINT" 2>/dev/null && break
  sleep 0.3
done
kill "$APID" 2>/dev/null; wait "$APID" 2>/dev/null
echo "checkpoint after reboot:"; cat "$PRAETOR_CHECKPOINT"; echo
[ -f "$OUT" ] && { echo "FAIL: output written before reboot"; exit 1; }

RESUME_AT=$(python3 -c "import json;print(json.load(open('$PRAETOR_CHECKPOINT'))['resume_at'])")
python3 -c "import json;json.dump(json.load(open('$PRAETOR_CHECKPOINT'))['vars'],open('restored.json','w'))"
echo "resume_at = $RESUME_AT"; echo

echo "=== 2. resume WITHOUT restored vars (expect failure: value lost) ==="
ansible-playbook play.yml --start-at-task="$RESUME_AT" -e outfile="$OUT" >/dev/null 2>&1
echo "exit=$? (non-zero expected)"
[ -f "$OUT" ] && { echo "FAIL: output written without restore"; exit 1; } || echo "no output written (as expected)"
echo

echo "=== 3. resume WITH restored vars (expect success: value restored) ==="
ansible-playbook play.yml --start-at-task="$RESUME_AT" -e @restored.json -e outfile="$OUT" >/dev/null 2>&1
rc=$?
echo "exit=$rc (zero expected)"
echo "output file:"; cat "$OUT" 2>/dev/null
if [ "$rc" -eq 0 ] && grep -q "greeting=value-from-before-crash" "$OUT" 2>/dev/null; then
  echo "=== RESULT: PASS — task 1 skipped, registered value restored across resume ==="
else
  echo "=== RESULT: FAIL ==="; exit 1
fi
