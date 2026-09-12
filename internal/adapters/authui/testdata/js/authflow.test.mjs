// Behavioural tests for the built-in auth UI page scripts (#1527).
//
// Each test runs the real inline script from the template against a fake Kratos
// that reproduces the two production behaviours the pages used to ignore:
// an expired flow answers 410, and creating a flow rotates the anti-CSRF token.
//
// Run via `make check` (target: check-authui-js).

import test from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { loadPage, fakeKratos, jsonResponse } from './harness.mjs';

const templates = path.resolve(fileURLToPath(new URL('.', import.meta.url)), '../../templates');
const loginHTML = path.join(templates, 'login.html');
const registrationHTML = path.join(templates, 'registration.html');
const settingsHTML = path.join(templates, 'settings.html');

const credentials = { byName: { identifier: 'user@example.com', password: 'correct horse' } };

function oidcNode(provider) {
  return {
    group: 'oidc',
    attributes: { name: 'provider', type: 'submit', value: provider },
    meta: { label: { text: provider } },
  };
}

function errorText(page) {
  const box = page.el('error-box');
  return box.style.display === 'block' ? box.textContent : '';
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

test('login: page load with an expired ?flow= starts a fresh flow instead of erroring', async () => {
  const kratos = fakeKratos('login');
  const page = await loadPage(loginHTML, {
    url: 'https://app.test/_vibewarden/login?flow=long-gone',
    fetch: kratos.fetch,
  });

  assert.equal(kratos.inits, 1, 'a fresh browser flow must be initialised');
  assert.equal(page.el('flow-id').value, 'flow-1');
  assert.equal(page.el('csrf-token').value, 'csrf-1');
  assert.equal(errorText(page), '');
});

test('login: flow expired while the page sat open — submit self-recovers', async () => {
  const kratos = fakeKratos('login');
  const page = await loadPage(loginHTML, { url: 'https://app.test/_vibewarden/login', fetch: kratos.fetch });
  assert.equal(page.el('csrf-token').value, 'csrf-1');

  // The lifespan elapses while the page is idle.
  kratos.expireAll();

  await page.submit('login-form', credentials);

  assert.equal(kratos.posts.length, 1, 'exactly one submit');
  assert.equal(kratos.posts[0].body.csrf_token, 'csrf-2', 'posts the re-initialised flow token, not the page-load one');
  assert.equal(kratos.posts[0].body.identifier, 'user@example.com', 'keeps the typed identifier');
  assert.deepEqual(page.navigations, ['/'], 'login succeeds and navigates to return_to');
  assert.equal(errorText(page), '', 'the user is never asked to refresh');
});

test('login: flow dies between the pre-submit fetch and the POST — retries once', async () => {
  const kratos = fakeKratos('login');
  let armed = false; // armed only after the page has loaded its first flow
  const racyFetch = async (url, opts) => {
    const res = await kratos.fetch(url, opts);
    if (armed && url.includes('/login/flows?id=')) {
      armed = false;
      kratos.expireAll(); // expires after the page read it, before it posts
    }
    return res;
  };

  const page = await loadPage(loginHTML, { url: 'https://app.test/_vibewarden/login', fetch: racyFetch });
  armed = true;
  await page.submit('login-form', credentials);

  assert.equal(kratos.posts.length, 2, 'one rejected POST, one retry');
  assert.equal(kratos.posts[0].body.csrf_token, 'csrf-1');
  assert.equal(kratos.posts[1].body.csrf_token, 'csrf-2');
  assert.deepEqual(page.navigations, ['/']);
  assert.equal(errorText(page), '');
});

test('login: security_csrf_violation is recovered, not rendered as dead state', async () => {
  const kratos = fakeKratos('login');
  const page = await loadPage(loginHTML, { url: 'https://app.test/_vibewarden/login', fetch: kratos.fetch });

  // Another flow rotates the anti-CSRF cookie; flow-1 is still alive but its
  // token is now rejected. This is the failure reported in #1527.
  kratos.newFlow();

  await page.submit('login-form', credentials);

  assert.equal(kratos.posts.length, 2, 'rejected POST plus one retry');
  assert.equal(kratos.posts[1].body.csrf_token, 'csrf-3', 'retry uses a freshly minted token');
  assert.deepEqual(page.navigations, ['/']);
  assert.notEqual(page.el('flow-id').value, '', 'an error envelope must never blank the flow id');
  assert.equal(errorText(page), '');
});

test('login: a real credential error is shown once and is not retried', async () => {
  const kratos = fakeKratos('login');
  kratos.success = (flowID) => {
    const flow = kratos.flows.get(flowID);
    flow.ui.messages = [{ id: 4000006, type: 'error', text: 'The provided credentials are invalid.' }];
    return jsonResponse(400, flow);
  };

  const page = await loadPage(loginHTML, { url: 'https://app.test/_vibewarden/login', fetch: kratos.fetch });
  await page.submit('login-form', credentials);

  assert.equal(kratos.posts.length, 1, 'a validation error must not trigger a flow retry');
  assert.match(errorText(page), /credentials are invalid/);
  assert.equal(page.navigations.length, 0);
});

test('login: re-rendering a flow does not duplicate the social buttons', async () => {
  const kratos = fakeKratos('login');
  kratos.newFlow = ((orig) => () => {
    const flow = orig();
    flow.ui.nodes.push(oidcNode('github'));
    return flow;
  })(kratos.newFlow);
  kratos.success = (flowID) => jsonResponse(400, kratos.flows.get(flowID));

  const page = await loadPage(loginHTML, { url: 'https://app.test/_vibewarden/login', fetch: kratos.fetch });
  assert.equal(page.el('social-buttons').children.length, 1);

  await page.submit('login-form', credentials);
  assert.equal(page.el('social-buttons').children.length, 1, 'applyFlow replaces the buttons rather than appending');
});

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

const registrationInput = {
  byName: { 'traits.email': 'new@example.com' },
  byID: { password: 'correct horse', 'password-confirm': 'correct horse' },
};

test('registration: expired flow on submit self-recovers with a fresh token', async () => {
  const kratos = fakeKratos('registration');
  const page = await loadPage(registrationHTML, {
    url: 'https://app.test/_vibewarden/registration',
    fetch: kratos.fetch,
  });

  kratos.expireAll();
  await page.submit('reg-form', registrationInput);

  assert.equal(kratos.posts.length, 1);
  assert.equal(kratos.posts[0].body.csrf_token, 'csrf-2');
  assert.equal(kratos.posts[0].body['traits.email'], 'new@example.com', 'keeps the typed email');
  assert.deepEqual(page.navigations, ['/']);
  assert.equal(errorText(page), '');
});

test('registration: security_csrf_violation is retried once', async () => {
  const kratos = fakeKratos('registration');
  const page = await loadPage(registrationHTML, {
    url: 'https://app.test/_vibewarden/registration',
    fetch: kratos.fetch,
  });

  kratos.newFlow();
  await page.submit('reg-form', registrationInput);

  assert.equal(kratos.posts.length, 2);
  assert.equal(kratos.posts[1].body.csrf_token, 'csrf-3');
  assert.deepEqual(page.navigations, ['/']);
  assert.notEqual(page.el('flow-id').value, '');
});

test('registration: a field-level error is rendered and not retried', async () => {
  const kratos = fakeKratos('registration');
  kratos.success = (flowID) => {
    const flow = kratos.flows.get(flowID);
    flow.ui.messages = [{ type: 'error', text: 'That email address is already taken.' }];
    return jsonResponse(400, flow);
  };

  const page = await loadPage(registrationHTML, {
    url: 'https://app.test/_vibewarden/registration',
    fetch: kratos.fetch,
  });
  await page.submit('reg-form', registrationInput);

  assert.equal(kratos.posts.length, 1);
  assert.match(errorText(page), /already taken/);
});

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

const passwordChange = { byID: { 'new-password': 'a new secret', 'confirm-password': 'a new secret' } };

test('settings: expired flow on password change self-recovers', async () => {
  const kratos = fakeKratos('settings');
  const page = await loadPage(settingsHTML, { url: 'https://app.test/_vibewarden/settings', fetch: kratos.fetch });
  assert.equal(page.el('password-csrf-token').value, 'csrf-1');

  kratos.expireAll();
  await page.submit('password-form', passwordChange);

  assert.equal(kratos.posts.length, 1);
  assert.equal(kratos.posts[0].body.csrf_token, 'csrf-2');
  assert.equal(kratos.posts[0].body.password, 'a new secret');
  assert.equal(page.el('success-box').style.display, 'block');
  assert.equal(errorText(page), '');
});

test('settings: security_csrf_violation on password change is retried once', async () => {
  const kratos = fakeKratos('settings');
  const page = await loadPage(settingsHTML, { url: 'https://app.test/_vibewarden/settings', fetch: kratos.fetch });

  kratos.newFlow();
  await page.submit('password-form', passwordChange);

  assert.equal(kratos.posts.length, 2);
  assert.equal(kratos.posts[1].body.csrf_token, 'csrf-3');
  assert.equal(page.el('success-box').style.display, 'block');
});

test('settings: a policy error is rendered and not retried', async () => {
  const kratos = fakeKratos('settings');
  kratos.success = (flowID) => {
    const flow = kratos.flows.get(flowID);
    flow.ui.messages = [{ type: 'error', text: 'The password can not be used because it is too similar to the email.' }];
    return jsonResponse(400, flow);
  };

  const page = await loadPage(settingsHTML, { url: 'https://app.test/_vibewarden/settings', fetch: kratos.fetch });
  await page.submit('password-form', passwordChange);

  assert.equal(kratos.posts.length, 1);
  assert.match(errorText(page), /too similar/);
});
