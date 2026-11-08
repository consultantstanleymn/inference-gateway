// Fixed arrival rate load against the gRPC Classify method (k6 gRPC client).
// Env: ADDR (host:port), RATE, DURATION
import grpc from 'k6/net/grpc';
import { check } from 'k6';

const client = new grpc.Client();
client.load(['../proto'], 'inference/v1/inference.proto');
const rate = parseInt(__ENV.RATE || '50');

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: rate,
      timeUnit: '1s',
      duration: __ENV.DURATION || '30s',
      preAllocatedVUs: 50,
      maxVUs: 400,
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  if (__ITER === 0) client.connect(__ENV.ADDR || '127.0.0.1:9090', { plaintext: true });
  const res = client.invoke('inference.v1.Gateway/Classify', { text: 'My card has not arrived yet' });
  check(res, { 'status OK': (r) => r && r.status === grpc.StatusOK });
}

export function handleSummary(data) {
  const m = data.metrics;
  const d = m.grpc_req_duration.values;
  const out = {
    rate_target: rate,
    requests: m.iterations.values.count,
    throughput_rps: +m.iterations.values.rate.toFixed(1),
    checks_rate: m.checks.values.rate,
    dropped_iterations: m.dropped_iterations ? m.dropped_iterations.values.count : 0,
    avg_ms: +d.avg.toFixed(2), p50_ms: +d.med.toFixed(2), p95_ms: +d['p(95)'].toFixed(2), p99_ms: +d['p(99)'].toFixed(2), max_ms: +d.max.toFixed(2),
  };
  return { stdout: 'K6SUMMARY ' + JSON.stringify(out) + '\n' };
}
