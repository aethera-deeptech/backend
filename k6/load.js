// A steady load that runs nightly and on demand. The gateways send a batch every 10 seconds and the owner opens screens at 30 a second,
// which is far above what one business does, so the run shows how the service holds when the screens and the ingest meet.
import { gatewayScenarios } from './lib.js';

export { gateway, owner } from './lib.js';

// The preallocated VUs cover a stall of about two seconds on a shared runner. k6 drops an iteration when no VU is free at its start.
export const options = {
  scenarios: {
    ...gatewayScenarios('3m15s'),
    owner: {
      executor: 'ramping-arrival-rate',
      exec: 'owner',
      startRate: 5,
      timeUnit: '1s',
      preAllocatedVUs: 60,
      maxVUs: 120,
      stages: [
        { target: 30, duration: '30s' },
        { target: 30, duration: '2m30s' },
        { target: 0, duration: '15s' },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    dropped_iterations: ['count<1'],
    'http_req_duration{endpoint:ingest}': ['p(95)<300'],
    'http_req_duration{endpoint:beranda}': ['p(95)<400'],
    'http_req_duration{endpoint:devices}': ['p(95)<400'],
    'http_req_duration{endpoint:device}': ['p(95)<400'],
    'http_req_duration{endpoint:recommendations}': ['p(95)<500'],
    'http_req_duration{endpoint:production}': ['p(95)<300'],
    'http_req_duration{endpoint:reports}': ['p(95)<500'],
    'http_req_duration{endpoint:settings}': ['p(95)<300'],
  },
};
