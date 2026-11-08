#!/usr/bin/env bash
# Kubernetes end-to-end checks on a local kind cluster.
# Local: CONTAINER_TOOL=podman (default if docker is missing). CI: CONTAINER_TOOL=docker.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER=${CLUSTER:-ig-e2e}
TOOL=${CONTAINER_TOOL:-$(docker info >/dev/null 2>&1 && echo docker || echo podman)}
IMAGE=localhost/inference-gateway:dev
CHART=deploy/helm/inference-gateway
REPORT=${REPORT:-out/e2e-report.txt}
K6_IMAGE=docker.io/grafana/k6:1.0.0
MS_URL=https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.7.2/components.yaml
[ "$TOOL" = podman ] && export KIND_EXPERIMENTAL_PROVIDER=podman
mkdir -p out; : > "$REPORT"
say() { echo "$*" | tee -a "$REPORT"; }
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }
PF_PIDS=()
cleanup() { for p in "${PF_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

step_static() {
  say "== helm lint and kubeconform"
  helm lint "$CHART" -f "$CHART/values-model-v1.yaml" | tee -a "$REPORT"
  if command -v kubeconform >/dev/null; then
    for v in v1 v2; do
      helm template ig "$CHART" -f "$CHART/values-model-$v.yaml" | kubeconform -strict -summary | tee -a "$REPORT"
    done
  elif [ -n "${CI:-}" ]; then echo "kubeconform missing in CI"; exit 1
  else say "SKIPPED kubeconform (not installed locally, CI runs it)"; fi
}

step_cluster() {
  say "== kind cluster ($TOOL)"
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then kind create cluster --name "$CLUSTER" --wait 240s; fi
  kubectl config use-context "kind-$CLUSTER" >/dev/null
  if [ "$TOOL" = podman ]; then
    rm -f out/ig-image.tar
    "$TOOL" save "$IMAGE" -o out/ig-image.tar
    kind load image-archive out/ig-image.tar --name "$CLUSTER"
  else kind load docker-image "$IMAGE" --name "$CLUSTER"; fi
  say "== metrics-server"
  curl -fsSL "$MS_URL" | sed 's|- --metric-resolution=15s|- --metric-resolution=15s\n        - --kubelet-insecure-tls|' | kubectl apply -f - >/dev/null
  kubectl -n kube-system rollout status deploy/metrics-server --timeout=180s
}

helm_ig() { # helm_ig <model version> [extra helm args]
  local v=$1; shift
  helm upgrade --install ig "$CHART" -f "$CHART/values-model-$v.yaml" \
    --set image.repository=localhost/inference-gateway --set image.tag=dev --set gateway.warmupHold=6s "$@" \
    --wait --timeout 240s
}

step_install_and_smoke() {
  say "== helm install model v1"
  helm_ig v1
  kubectl rollout status deploy/inference-gateway --timeout=120s | tee -a "$REPORT"
  go build -o bin/probe ./cmd/probe
  local hp gp
  hp=$(free_port); gp=$(free_port)
  kubectl port-forward svc/inference-gateway "$hp:8080" >/dev/null 2>&1 & PF_PIDS+=($!)
  kubectl port-forward svc/inference-gateway "$gp:9090" >/dev/null 2>&1 & PF_PIDS+=($!)
  for _ in $(seq 40); do curl -sf "localhost:$hp/readyz" >/dev/null && break; sleep 0.5; done
  say "== smoke over HTTP"
  bin/probe http -addr "127.0.0.1:$hp" -text "I think my card was stolen" | tee -a "$REPORT"; echo | tee -a "$REPORT"
  say "== smoke over gRPC"
  bin/probe grpc -addr "127.0.0.1:$gp" -text "I think my card was stolen" | tee -a "$REPORT"
  cleanup; PF_PIDS=()
}

run_k6_job() { # run_k6_job <name> <script> <env...>  -> prints K6SUMMARY json
  local name=$1 script=$2; shift 2
  kubectl delete job "$name" --ignore-not-found >/dev/null
  kubectl delete configmap "$name-script" --ignore-not-found >/dev/null
  kubectl create configmap "$name-script" --from-file="script.js=loadtest/$script" >/dev/null
  local envs=""
  for e in "$@"; do envs+="        - {name: ${e%%=*}, value: \"${e#*=}\"}"$'\n'; done
  kubectl apply -f - >/dev/null <<YAML
apiVersion: batch/v1
kind: Job
metadata: {name: $name}
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      volumes: [{name: s, configMap: {name: $name-script}}]
      containers:
      - name: k6
        image: $K6_IMAGE
        args: ["run", "/s/script.js"]
        volumeMounts: [{name: s, mountPath: /s}]
        env:
$envs
YAML
}

wait_k6() { # wait_k6 <name> <timeout seconds>
  kubectl wait --for=condition=complete "job/$1" --timeout="${2}s" >/dev/null || { kubectl logs "job/$1" | tail -20; return 1; }
  kubectl logs "job/$1" | grep K6SUMMARY | sed 's/^K6SUMMARY //'
}

step_rolling_upgrade() {
  for _ in $(seq "${ROLLING_REPEATS:-1}"); do rolling_once || return 1; done
}

rolling_once() {
  local cur target
  cur=$(kubectl get configmap inference-gateway -o jsonpath='{.data.GATEWAY_MODEL_DIR}' | sed 's|/models/||')
  if [ "$cur" = v1 ]; then target=v2; else target=v1; fi
  say "== rolling upgrade $cur -> $target under load (10 VUs, readiness watched)"
  run_k6_job k6-rolling rolling.js URL=http://inference-gateway:8080/v1/classify VUS=10 DURATION=75s
  sleep 15
  helm_ig "$target" &
  local hpid=$! observed=0 violations=0
  while kill -0 "$hpid" 2>/dev/null; do
    while read -r ip; do
      [ -z "$ip" ] && continue
      observed=$((observed+1))
      if kubectl get endpointslices -l kubernetes.io/service-name=inference-gateway -o json |
         jq -e --arg ip "$ip" '[.items[].endpoints[]? | select(.conditions.ready==true) | .addresses[]] | index($ip)' >/dev/null; then
        # The two reads are not atomic. Count a violation only if the pod is still not ready now.
        if kubectl get pods -l app.kubernetes.io/name=inference-gateway -o json |
           jq -e --arg ip "$ip" '[.items[] | select(.status.podIP==$ip and ([.status.containerStatuses[]?.ready]|all|not))] | length > 0' >/dev/null; then
          violations=$((violations+1))
        fi
      fi
    done < <(kubectl get pods -l app.kubernetes.io/name=inference-gateway -o json |
      jq -r '.items[] | select(.metadata.deletionTimestamp==null and .status.phase=="Running" and ([.status.containerStatuses[]?.ready]|all|not)) | .status.podIP')
    sleep 0.5
  done
  wait "$hpid"
  local out; out=$(wait_k6 k6-rolling 120)
  say "k6 under upgrade: $out"
  say "readiness samples (running but not ready): $observed, of which receiving traffic: $violations"
  echo "$out" | jq -e '.non_2xx==0 and .failed_rate==0 and .v1>0 and .v2>0' >/dev/null || { say "FAIL rolling upgrade"; return 1; }
  say "$(echo "$out" | jq -r --arg t "$target" 'if $t=="v2" then "responses moved v1 -> v2" else "responses moved v2 -> v1" end')"
  [ "$observed" -gt 0 ] && [ "$violations" -eq 0 ] || { say "FAIL readiness (observed=$observed violations=$violations)"; return 1; }
  say "PASS rolling upgrade $cur -> $target: zero non-2xx, no traffic before ready"
}

step_hpa() {
  say "== HPA scale-up under load"
  local min; min=$(kubectl get hpa inference-gateway -o jsonpath='{.spec.minReplicas}')
  # Start from an idle autoscaler, so a scale-up cannot be left over from earlier load.
  local idle; idle=$(kubectl get hpa inference-gateway -o jsonpath='{.status.desiredReplicas}')
  [ "${idle:-0}" -le "$min" ] || { say "FAIL HPA precondition: desired $idle is above min $min before load"; return 1; }
  local cur; cur=$(kubectl get deploy inference-gateway -o jsonpath='{.status.replicas}')
  [ "${cur:-0}" -le "$min" ] || { say "FAIL HPA precondition: running $cur replicas, min $min"; return 1; }
  say "HPA before load: running $cur, desired $idle, min $min"
  for attempt in 1 2; do
    run_k6_job k6-hpa rolling.js URL=http://inference-gateway:8080/v1/classify VUS=60 DURATION=170s
    local ok=0 t0=$SECONDS
    while [ $((SECONDS-t0)) -lt 180 ]; do
      local want; want=$(kubectl get hpa inference-gateway -o jsonpath='{.status.desiredReplicas}')
      if [ "${want:-0}" -gt "$min" ]; then ok=1; say "HPA desired replicas $want (min $min) after $((SECONDS-t0)) s, attempt $attempt"; break; fi
      sleep 5
    done
    kubectl delete job k6-hpa --ignore-not-found >/dev/null
    [ $ok -eq 1 ] && { say "PASS HPA"; return 0; }
    say "HPA did not scale on attempt $attempt"
  done
  say "FAIL HPA"; return 1
}

step_trace() {
  say "== trace check with OpenTelemetry Collector (file exporter)"
  kubectl apply -f deploy/e2e/otel-collector.yaml >/dev/null
  kubectl rollout status deploy/otel-collector --timeout=180s
  helm_ig v2 --set otel.endpoint=http://otel-collector:4317
  local hp; hp=$(free_port)
  kubectl port-forward svc/inference-gateway "$hp:8080" >/dev/null 2>&1 & PF_PIDS+=($!)
  for _ in $(seq 40); do curl -sf "localhost:$hp/readyz" >/dev/null && break; sleep 0.5; done
  for _ in 1 2 3; do bin/probe http -addr "127.0.0.1:$hp" -text "trace me" >/dev/null; done
  cleanup; PF_PIDS=()
  for _ in $(seq 20); do
    if kubectl logs deploy/otel-collector -c tail | python3 scripts/check_trace.py > out/trace-check.txt; then
      say "$(cat out/trace-check.txt)"; return 0
    fi
    sleep 3
  done
  say "FAIL trace: $(cat out/trace-check.txt)"; return 1
}

step_health_pdb() {
  say "== pdb and probes present"
  kubectl get pdb inference-gateway | tee -a "$REPORT"
  kubectl get deploy inference-gateway -o json | jq '.spec.template.spec.containers[0] | {readinessProbe, livenessProbe, resources, securityContext}' | tee -a "$REPORT" >/dev/null
}

STEPS=${*:-static cluster install_and_smoke hpa rolling_upgrade trace health_pdb}
for s in $STEPS; do "step_$s"; done
say "ALL REQUESTED STEPS FINISHED"
