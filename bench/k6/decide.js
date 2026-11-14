import http from 'k6/http';
import { check } from 'k6';
import { SharedArray } from 'k6/data';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const AGENT = __ENV.AGENT || 'bench-agent';
const API_KEY = __ENV.API_KEY || '';
const RATE = parseInt(__ENV.RATE || '500', 10);
const DURATION = __ENV.DURATION || '60s';
const MAX_VUS = parseInt(__ENV.MAX_VUS || '400', 10);

const corpus = new SharedArray('corpus', () =>
  open('../corpus.jsonl').trim().split('\n').map((line) => JSON.parse(line)),
);
const manifest = open('../../internal/bench/agent.yaml');

const decisionMs = new Trend('decision_latency_ms', true);
const attackCaught = new Rate('attack_caught');
const falsePositive = new Rate('false_positive');
const decided = new Counter('decisions');

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  scenarios: {
    decide: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.min(50, MAX_VUS),
      maxVUs: MAX_VUS,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<50', 'p(99)<150'],
    decision_latency_ms: ['p(95)<25'],
    attack_caught: ['rate>0.9'],
    false_positive: ['rate<0.02'],
  },
};

function headers() {
  const h = { 'Content-Type': 'application/json', 'X-Rungated-Agent': AGENT };
  if (API_KEY) {
    h.Authorization = `Bearer ${API_KEY}`;
  }
  return h;
}

function payload(c, session) {
  const r = { agent_id: AGENT, session_id: session, kind: c.kind };
  if (c.parts && c.parts.length) r.texts = c.parts;
  if (c.kind === 'llm.request') {
    r.model = 'mock-1';
    r.provider = 'mock';
  }
  if (c.server) r.server = c.server;
  if (c.tool) r.tool = c.tool;
  if (c.args) r.args = c.args;
  if (c.method) r.method = c.method;
  if (c.host) r.host = c.host;
  if (c.url) r.url = c.url;
  return JSON.stringify(r);
}

export function setup() {
  const h = { 'Content-Type': 'application/yaml' };
  if (API_KEY) h.Authorization = `Bearer ${API_KEY}`;
  const res = http.post(`${BASE_URL}/v1/agents`, manifest, { headers: h });
  if (res.status !== 200 && res.status !== 201) {
    throw new Error(`could not register the bench agent: ${res.status} ${res.body}`);
  }
}

const verdictRank = { allow: 0, warn: 1, redact: 2, hold: 3, block: 4, contain: 5 };

export default function () {
  const c = corpus[Math.floor(Math.random() * corpus.length)];
  const session = `k6-${c.id}-${__VU}-${__ITER}`;
  const res = http.post(`${BASE_URL}/v1/decide`, payload(c, session), { headers: headers(), tags: { class: c.class } });
  const ok = check(res, { 'status is 200': (r) => r.status === 200 });
  if (!ok) return;
  const body = res.json();
  decided.add(1);
  decisionMs.add((body.latency_us || 0) / 1000);
  const rank = verdictRank[body.computed] || 0;
  if (c.label === 'attack') {
    attackCaught.add(rank >= verdictRank.redact);
  } else {
    falsePositive.add(rank >= verdictRank.hold);
  }
}