// A 30 second check that runs on every pull request. The limits are loose, because a shared runner is noisy.
import { gatewayScenarios } from './lib.js';

export { gateway, owner } from './lib.js';

export const options = {
  scenarios: {
    ...gatewayScenarios('30s'),
    owner: { executor: 'constant-arrival-rate', exec: 'owner', rate: 5, timeUnit: '1s', duration: '30s', preAllocatedVUs: 10, maxVUs: 20 },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    dropped_iterations: ['count<1'],
    'http_req_duration{endpoint:ingest}': ['p(95)<400'],
    'http_req_duration{endpoint:beranda}': ['p(95)<500'],
    'http_req_duration{endpoint:devices}': ['p(95)<500'],
    'http_req_duration{endpoint:device}': ['p(95)<500'],
    'http_req_duration{endpoint:recommendations}': ['p(95)<600'],
  },
};
