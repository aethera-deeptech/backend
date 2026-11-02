// Shared helpers and the scenarios of smoke.js and load.js. The data comes from prepare.py, which writes setup.json before k6 starts.
import http from 'k6/http';
import { check, sleep } from 'k6';

/* global __ENV, open */

const base = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const origin = __ENV.APP_ORIGIN || 'http://localhost:5173';
const data = JSON.parse(open('./setup.json'));

// seconds between two batches of one gateway. The service allows 600 batches in an hour for each gateway, which is one every 6 seconds
const batchEvery = 10;

// the sequence number is kept for each VU, and one VU plays one gateway for the whole test
let seq = 0;

// screens is what the app reads when the owner opens it, with the weight of each in the mix
const screens = [
  { name: 'beranda', path: '/v1/beranda', weight: 4 },
  { name: 'devices', path: '/v1/devices', weight: 4 },
  { name: 'device', path: null, weight: 3 },
  { name: 'recommendations', path: '/v1/recommendations', weight: 2 },
  { name: 'production', path: '/v1/production', weight: 1 },
  { name: 'reports', path: '/v1/reports', weight: 1 },
  { name: 'settings', path: '/v1/settings', weight: 1 },
];

const total = screens.reduce((sum, s) => sum + s.weight, 0);

function pick() {
  let roll = Math.random() * total;

  for (const s of screens) {
    roll -= s.weight;
    if (roll < 0) {
      return s;
    }
  }

  return screens[0];
}

// gateway plays one gateway that sends a batch of its plugs every few seconds
export function gateway() {
  const g = data.gateways[Number(__ENV.GW)];

  if (seq === 0) {
    seq = g.seq;

    // real gateways are not in step, so each one starts at its own point of the interval and the first batches do not arrive together
    sleep((Number(__ENV.GW) / data.gateways.length) * batchEvery);
  }

  seq += 1;
  const now = Math.floor(Date.now() / 1000);
  const nodes = g.nodes.map((nodeId) => ({
    nodeId,
    kind: 'plug',
    samples: [4, 2, 0].map((ago) => ({ t: now - ago, p: Math.round((60 + Math.random() * 400) * 10) / 10 })),
  }));

  const res = http.post(`${base}/device/v1/readings`, JSON.stringify({ seq, sentAt: now, nodes }), {
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${g.key}` },
    tags: { endpoint: 'ingest' },
  });

  check(res, {
    'ingest answers 200': (r) => r.status === 200,
    'ingest accepts every sample': (r) => r.status !== 200 || r.json('accepted') === nodes.length * 3,
  });

  sleep(batchEvery);
}

// owner reads one screen of the app
export function owner() {
  const s = pick();
  const path = s.path || `/v1/devices/${data.devices[Math.floor(Math.random() * data.devices.length)]}`;

  const res = http.get(`${base}${path}`, {
    headers: { Cookie: data.cookie, 'X-Requested-With': 'aethera', Origin: origin },
    tags: { endpoint: s.name },
  });

  check(res, { [`${s.name} answers 200`]: (r) => r.status === 200 });
}

// gatewayScenarios makes one scenario for each gateway, with one VU that plays it for the whole duration. A scenario of its own and not
// a shared one, because k6 does not promise which VU id belongs to which scenario.
export function gatewayScenarios(duration) {
  const out = {};

  data.gateways.forEach((_, index) => {
    out[`gateway_${index}`] = { executor: 'constant-vus', exec: 'gateway', vus: 1, duration, env: { GW: String(index) } };
  });

  return out;
}
