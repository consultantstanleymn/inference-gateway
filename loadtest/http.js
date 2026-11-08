// Fixed arrival rate load against POST /v1/classify.
// Env: URL, RATE (req/s), DURATION (e.g. 30s), VUS (max pre-allocated), WARMUP (e.g. 5s)
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const url = __ENV.URL || 'http://127.0.0.1:8080/v1/classify';
const rate = parseInt(__ENV.RATE || '50');
const v1 = new Counter('model_v1_responses');
const v2 = new Counter('model_v2_responses');
const shed = new Counter('shed_429');

const texts = [
  'My card has not arrived yet',
  'Why was I charged a fee for a transfer',
  'How do I top up my account with a card',
  'I forgot my passcode',
  'The exchange rate looks wrong',
  'Is there a limit on cash withdrawals',
  'I want to cancel a transfer I just made',
  'Where can I use my virtual card',
];

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: rate,
      timeUnit: '1s',
      duration: __ENV.DURATION || '30s',
      preAllocatedVUs: parseInt(__ENV.VUS || '50'),
      maxVUs: parseInt(__ENV.MAXVUS || '400'),
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const t = texts[Math.floor(Math.random() * texts.length)];
  const res = http.post(url, JSON.stringify({ text: t }), { headers: { 'Content-Type': 'application/json' } });
  if (res.status === 429) shed.add(1);
  const ok = check(res, { 'status 200': (r) => r.status === 200 });
  if (ok) {
    const mv = res.json('model_version');
    if (mv === 'v1') v1.add(1);
    if (mv === 'v2') v2.add(1);
  }
}

export function handleSummary(data) {
  const m = data.metrics;
  const d = m.http_req_duration.values;
  const out = {
    rate_target: rate,
    requests: m.http_reqs.values.count,
    throughput_rps: +m.http_reqs.values.rate.toFixed(1),
    failed_rate: m.http_req_failed.values.rate,
    shed_429: m.shed_429 ? m.shed_429.values.count : 0,
    v1: m.model_v1_responses ? m.model_v1_responses.values.count : 0,
    v2: m.model_v2_responses ? m.model_v2_responses.values.count : 0,
    dropped_iterations: m.dropped_iterations ? m.dropped_iterations.values.count : 0,
    avg_ms: +d.avg.toFixed(2), p50_ms: +d.med.toFixed(2), p95_ms: +d['p(95)'].toFixed(2), p99_ms: +d['p(99)'].toFixed(2), max_ms: +d.max.toFixed(2),
  };
  return { stdout: 'K6SUMMARY ' + JSON.stringify(out) + '\n' };
}
