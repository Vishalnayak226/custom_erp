'use strict';
// 47.9.4 -> 47.17.3: how a browser budget run becomes the release gate's evidence.
//   node --test docs/qa/perf-budget-gate.test.cjs
const test = require('node:test');
const assert = require('node:assert');
const { gateEvidence } = require('./perf-budget.cjs');

const declared = ['desktop-cable', 'throttled-4x', 'phone-low'];
const run = (verdict, misses = [], known = []) => ({ verdict, misses, known, env: { browser: '130.0' } });

test('PASS on every declared profile is pass', () => {
  const e = gateEvidence(run('PASS'), declared, declared, true);
  assert.strictEqual(e.result, 'pass');
});
test('MISS and KNOWN are fail, never pass', () => {
  const m = { scope: 'desktop-cable', key: 'lcpP75Ms', value: 1800 };
  assert.strictEqual(gateEvidence(run('MISS', [m]), declared, declared, true).result, 'fail');
  const e = gateEvidence(run('KNOWN', [], [m]), declared, declared, true);
  assert.strictEqual(e.result, 'fail');
  assert.match(e.note, /first: desktop-cable\.lcpP75Ms=1800/);
});
test('a partial run does not stand for the candidate', () => {
  assert.ok(gateEvidence(run('PASS'), declared, ['desktop-cable'], true).skip);
  assert.ok(gateEvidence(run('PASS'), declared, declared, false).skip);
});
