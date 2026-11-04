'use strict';

// Source-level workflow checks for the offline prototype.
// This deliberately small DOM stub does not verify browser rendering or real authentication.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const prototypePath = path.join(__dirname, '..', 'workflow-wireframe.html');
const html = fs.readFileSync(prototypePath, 'utf8');
const scriptMatch = html.match(/<script>([\s\S]*)<\/script>/);
assert(scriptMatch, 'Expected an inline prototype script');

const elements = new Map();
function element(selector) {
  if (!elements.has(selector)) {
    elements.set(selector, {
      innerHTML: '',
      textContent: '',
      value: '电脑',
      style: {},
      handlers: {},
      addEventListener(type, callback) {
        this.handlers[type] = callback;
      }
    });
  }
  return elements.get(selector);
}

let currentHash = '';
const location = {
  get hash() {
    return currentHash;
  },
  set hash(value) {
    currentHash = value.startsWith('#') ? value : '#' + value;
  }
};

const context = {
  document: { querySelector: element, querySelectorAll: () => [] },
  location,
  window: { addEventListener() {}, scrollTo() {} },
  setTimeout: () => 0,
  clearTimeout() {},
  FormData: class {
    constructor(target) {
      this.target = target;
    }
    get(name) {
      return this.target[name];
    }
  }
};
vm.createContext(context);
vm.runInContext(scriptMatch[1], context, { filename: prototypePath });

function run(code) {
  return vm.runInContext(code, context);
}
function submit(selector, target, decision) {
  element(selector).handlers.submit({
    preventDefault() {},
    target,
    submitter: { value: decision }
  });
}
function mainIncludes(text) {
  return element('#main').innerHTML.includes(text);
}

assert(mainIncludes('商户列表'));
run("identity = 'financeA'; renderApprovals()");
assert(mainIncludes('A-EXAMPLE-001'));
assert(!mainIncludes('A-EXAMPLE-002'));
run("identity = 'financeB'; renderApprovals()");
assert(mainIncludes('A-EXAMPLE-002'));
assert(!mainIncludes('A-EXAMPLE-001'));
run("renderApproval('A-EXAMPLE-001')");
assert(mainIncludes('无权访问'));

// Create a merchant and check the incomplete-materials path.
run("identity = 'operator'; renderCreateMerchant()");
submit('#merchant-form', { name: '评审商户丙', contact: '评审联系人' });
assert.equal(run('merchants.length'), 3);
assert.equal(location.hash, '#merchant/M-EXAMPLE-003');
run("renderCreateOrder('M-EXAMPLE-003')");
submit('#order-form', { direction: 'd2f', amount: '20.00' });
assert.equal(run('orders[2].scenario'), 'materials');
run("renderRequest('O-EXAMPLE-003')");
assert(mainIncludes('id="demo-submit" disabled'));
const approvalCountBeforeBlockedSubmit = run('approvals.length');
element('#demo-submit').handlers.click();
assert.equal(run('approvals.length'), approvalCountBeforeBlockedSubmit);

// Submit against v1, then save v2 and verify the old submission stays fixed.
run("renderCreateOrder('M-EXAMPLE-001')");
submit('#order-form', { direction: 'f2d', amount: '25.00' });
run("renderRequest('O-EXAMPLE-004')");
element('#demo-submit').handlers.click();
assert.equal(run('orders[3].policySnapshot.version'), 1);
assert.equal(run('orders[3].policySnapshot.finance'), 'financeA');
assert.equal(run('approvals[2].owner'), 'financeA');
assert.equal(run('Object.isFrozen(orders[3].policySnapshot)'), true);

run('renderSettings()');
submit('#settings-form', {
  finance: 'financeB',
  eoThreshold: '20000.00',
  testAmount: '5.000000'
});
assert.equal(run('businessSettings.version'), 2);
assert.equal(run('businessSettings.finance'), 'financeB');
assert.equal(run('orders[3].policySnapshot.version'), 1);
assert.equal(run('orders[3].policySnapshot.finance'), 'financeA');
assert.equal(run('orders[3].policySnapshot.eoThreshold'), '10000.00');
assert.equal(run('orders[3].policySnapshot.testAmount'), '3.000000');
run("renderRequest('O-EXAMPLE-004')");
element('#demo-submit').handlers.click();
assert.equal(run('approvals.length'), 3, 'Repeated submission must not create another approval');

// New submissions use v2 and are assigned to the newly configured employee.
run("renderCreateOrder('M-EXAMPLE-001')");
submit('#order-form', { direction: 'd2f', amount: '30.000000' });
run("renderRequest('O-EXAMPLE-005')");
element('#demo-submit').handlers.click();
assert.equal(run('orders[4].policySnapshot.version'), 2);
assert.equal(run('orders[4].policySnapshot.finance'), 'financeB');
assert.equal(run('approvals[3].owner'), 'financeB');
run("identity = 'financeA'; renderApprovals()");
assert(mainIncludes('A-EXAMPLE-003'));
assert(!mainIncludes('A-EXAMPLE-004'));
run("identity = 'financeB'; renderApprovals()");
assert(mainIncludes('A-EXAMPLE-004'));
assert(!mainIncludes('A-EXAMPLE-003'));
run("renderApproval('A-EXAMPLE-003')");
assert(mainIncludes('无权访问'));

// Both simulated authentication steps are required before accepting.
run("location.hash = 'approval/A-EXAMPLE-004'; render()");
submit('#approval-form', {}, 'accepted');
assert.equal(run('approvals[3].status'), 'pending');
element('#demo-2fa').handlers.click();
element('#signature-device').value = '手机';
element('#demo-signature').handlers.click();
assert(element('#signature-state').textContent.includes('手机'));
submit('#approval-form', {}, 'accepted');
assert.equal(run('approvals[3].status'), 'accepted');
assert(mainIncludes('等待后续审批节点'));

run('renderSettings()');
submit('#settings-form', { finance: 'financeA', eoThreshold: '-10', testAmount: '3' });
assert.equal(run('businessSettings.version'), 2, 'Invalid settings must not create a version');
run("renderOrder('O-EXAMPLE-001')");
assert(mainIncludes('来源已扣款'));
run("renderOrder('O-EXAMPLE-002')");
assert(mainIncludes('原执行通道'));
assert(html.includes('平台收费本期不启用'));
assert(html.includes('HKD 仅'));

console.log('PASS: materials gate, versioned settings, historical/new submissions, duplicate submission, approval ownership, mobile signature placeholder, invalid settings, unresolved payment states.');
console.log('Scope: source and lightweight DOM logic only; browser rendering and real security are not covered.');
