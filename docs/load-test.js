import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://receiver:8080';
const JSON_PARAMS = { headers: { 'Content-Type': 'application/json' } };
const DURATION = '9m30s';

const GROUPS = {
  campaign: { count: 50, balance: 2000000 },
  small: { count: 3000, balance: 10000 },
  otp: { count: 300, balance: 10000 },
};

export const options = {
  setupTimeout: '5m',
  batchPerHost: 50,
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    campaigns: {
      executor: 'ramping-arrival-rate', exec: 'campaign', startRate: 0, timeUnit: '1s',
      preAllocatedVUs: 300, maxVUs: 2000,
      stages: [
        { target: 600, duration: '1m' },
        { target: 600, duration: '3m' },
        { target: 1100, duration: '30s' },
        { target: 1100, duration: '2m' },
        { target: 0, duration: '10s' },
        { target: 0, duration: '2m50s' },
      ],
    },
    small_customers: constant('small', 400),
    otp: constant('otp', 120),
    reports: constant('report', 20),
  },
};

function constant(exec, rate) {
  return {
    executor: 'constant-arrival-rate', exec, rate, timeUnit: '1s', duration: DURATION,
    preAllocatedVUs: 100, maxVUs: 1000,
  };
}

function createGroup(name, { count, balance }) {
  const ids = [];
  for (let i = 0; i < count; i += 200) {
    const n = Math.min(200, count - i);
    const res = http.batch(Array.from({ length: n }, (_, j) =>
      ['POST', `${BASE}/v1/customers`, JSON.stringify({ name: `${name}-${i + j}` }), JSON_PARAMS]));
    res.forEach((r) => ids.push(r.json('id')));
  }
  for (let i = 0; i < ids.length; i += 200) {
    http.batch(ids.slice(i, i + 200).map((id) =>
      ['POST', `${BASE}/v1/customers/${id}/topups`, JSON.stringify({ amount: balance }), JSON_PARAMS]));
  }
  return ids;
}

export function setup() {
  return Object.fromEntries(Object.entries(GROUPS).map(([name, g]) => [name, createGroup(name, g)]));
}

export function campaign(ids) {
  send(pick(ids.campaign), 'Weekend sale: 20% off everything', false);
}

export function small(ids) {
  send(pick(ids.small), 'Your order has shipped', false);
}

export function otp(ids) {
  send(pick(ids.otp), `Your code is ${Math.floor(1000 + Math.random() * 9000)}`, true);
}

export function report(ids) {
  const res = http.get(`${BASE}/v1/customers/${pick(ids.small)}/messages?limit=20`, { tags: { name: 'report' } });
  check(res, { 'report ok': (r) => r.status === 200 });
}

function send(customer, text, express) {
  const body = JSON.stringify({ client_id: crypto.randomUUID(), phone: '09121234567', text, express });
  const res = http.post(`${BASE}/v1/customers/${customer}/messages`, body,
    { ...JSON_PARAMS, tags: { name: express ? 'send express' : 'send normal' } });
  check(res, { accepted: (r) => r.status === 202 });
}

function pick(ids) {
  return ids[Math.floor(Math.random() * ids.length)];
}
