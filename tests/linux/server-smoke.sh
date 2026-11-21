#!/bin/bash
# Runs the Linux build of runtide-server inside WSL2 and exercises it over HTTP.
# usage: linux-server-test.sh <server-binary> <receipt.runtide.zip>
set -u
BIN="$1"; RCPT="$2"
cp "$BIN" /tmp/rts && chmod +x /tmp/rts
cp "$RCPT" /tmp/in.runtide.zip
rm -rf /tmp/rts-data
RUNTIDE_ADMIN_PASSWORD='Linux-Smoke-Only-98765' /tmp/rts --addr 127.0.0.1:18484 --data /tmp/rts-data >/tmp/rts.log 2>&1 &
PID=$!
for i in $(seq 1 30); do curl -fsS http://127.0.0.1:18484/healthz >/dev/null 2>&1 && break; sleep 0.5; done
echo "uname: $(uname -sr)"
echo "healthz: $(curl -fsS http://127.0.0.1:18484/healthz)"
echo "unauthenticated /projects: HTTP $(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18484/api/v1/projects)"
TOK=$(curl -fsS -X POST http://127.0.0.1:18484/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"Linux-Smoke-Only-98765"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
echo "login token length: ${#TOK}"
PID_JSON=$(curl -fsS -X POST http://127.0.0.1:18484/api/v1/projects -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"name":"shop"}')
PRJ=$(echo "$PID_JSON" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
echo "project: $PRJ"
echo "upload: $(curl -s -w ' HTTP %{http_code}' -X POST "http://127.0.0.1:18484/api/v1/projects/$PRJ/receipts" -H "Authorization: Bearer $TOK" -H 'Content-Type: application/zip' --data-binary @/tmp/in.runtide.zip | cut -c1-200)"
EXE=$(curl -fsS "http://127.0.0.1:18484/api/v1/projects/$PRJ/executions" -H "Authorization: Bearer $TOK" | sed -n 's/.*"id":"\(sx_[^"]*\)".*/\1/p' | head -1)
echo "stored execution: $EXE"
echo "inspect: $(curl -fsS "http://127.0.0.1:18484/api/v1/executions/$EXE" -H "Authorization: Bearer $TOK" | cut -c1-160)"
echo "report bytes: $(curl -fsS "http://127.0.0.1:18484/api/v1/executions/$EXE/report" -H "Authorization: Bearer $TOK" | wc -c)"
echo "report mentions integrity: $(curl -fsS "http://127.0.0.1:18484/api/v1/executions/$EXE/report" -H "Authorization: Bearer $TOK" | grep -c 'Integrity:')"
# A tampered archive must be refused.
head -c 2000 /tmp/in.runtide.zip > /tmp/bad.zip
echo "truncated upload: HTTP $(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:18484/api/v1/projects/$PRJ/receipts" -H "Authorization: Bearer $TOK" -H 'Content-Type: application/zip' --data-binary @/tmp/bad.zip)"
echo "--- server log (first lines)"; head -5 /tmp/rts.log | cut -c1-200
kill $PID 2>/dev/null; wait $PID 2>/dev/null
rm -rf /tmp/rts-data /tmp/rts /tmp/in.runtide.zip /tmp/bad.zip /tmp/rts.log
