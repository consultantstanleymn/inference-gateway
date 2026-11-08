// Closed-loop load (fixed number of virtual users) used while a rolling upgrade runs.
// Env: URL, VUS, DURATION
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const url = __ENV.URL || 'http://127.0.0.1:8080/v1/classify';
const v1 = new Counter('model_v1_responses');
const v2 = new Counter('model_v2_responses');
const bad = new Counter('non_2xx');

export const options = {
  vus: parseInt(__ENV.VUS || '10'),
  duration: __ENV.DURATION || '60s',
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const res = http.post(url, JSON.stringify({ text: 'I lost my card, what should I do' }), { headers: { 'Content-Type': 'application/json' }, timeout: '5s' });
  const ok = check(res, { 'status 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (!ok) { bad.add(1); return; }
  const mv = res.json('model_version');
  if (mv === 'v1') v1.add(1);
  if (mv === 'v2') v2.add(1);
}

export function handleSummary(data) {
  const m = data.metrics;
  const out = {
    requests: m.http_reqs.values.count,
    non_2xx: m.non_2xx ? m.non_2xx.values.count : 0,
    failed_rate: m.http_req_failed.values.rate,
    v1: m.model_v1_responses ? m.model_v1_responses.values.count : 0,
    v2: m.model_v2_responses ? m.model_v2_responses.values.count : 0,
    p95_ms: +m.http_req_duration.values['p(95)'].toFixed(2),
  };
  return { stdout: 'K6SUMMARY ' + JSON.stringify(out) + '\n' };
}
