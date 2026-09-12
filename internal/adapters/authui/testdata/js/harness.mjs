// Harness for executing the auth-UI page scripts outside a browser.
//
// The pages in internal/adapters/authui/templates are plain HTML with an inline
// vanilla-JS IIFE. Go tests can only assert on the rendered markup, which is how
// #1527 shipped: the login page looked fine and posted dead flow state. This
// harness runs the real inline script in a Node vm against a minimal DOM shim
// and a fake Kratos, so the tests exercise behaviour rather than string shape.
//
// Zero dependencies: node:test, node:vm, node:fs only.

import { readFileSync } from 'node:fs';
import vm from 'node:vm';

// ---------------------------------------------------------------------------
// DOM shim
// ---------------------------------------------------------------------------

class FakeElement {
  constructor(tagName, id = '') {
    this.tagName = tagName;
    this.id = id;
    this.name = '';
    this.type = '';
    this.value = '';
    this.textContent = '';
    this.className = '';
    this.style = {};
    this.dataset = {};
    this.children = [];
    this.classes = new Set();
    this.listeners = new Map();

    const classes = this.classes;
    this.classList = {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c),
    };
  }

  appendChild(child) {
    this.children.push(child);
    return child;
  }

  replaceChildren(...nodes) {
    this.children = nodes;
  }

  addEventListener(type, fn) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(fn);
  }

  // fire invokes every listener for `type` and awaits async handlers.
  async fire(type, event) {
    for (const fn of this.listeners.get(type) || []) {
      await fn(event);
    }
  }
}

// buildDocument creates one FakeElement per id="..." in the markup. An id the
// script references but the markup does not define yields null from
// getElementById, which throws inside the script and fails the test loudly.
function buildDocument(html) {
  const byID = new Map();
  const byName = new Map();

  for (const m of html.matchAll(/\bid="([^"]+)"/g)) {
    byID.set(m[1], new FakeElement('div', m[1]));
  }
  for (const tag of html.matchAll(/<input\b[^>]*>/g)) {
    const id = /\bid="([^"]+)"/.exec(tag[0]);
    const name = /\bname="([^"]+)"/.exec(tag[0]);
    if (!id || !name) continue;
    const el = byID.get(id[1]);
    el.tagName = 'input';
    el.name = name[1];
    byName.set(name[1], el);
  }

  return {
    byID,
    byName,
    getElementById: (id) => byID.get(id) || null,
    querySelector: (sel) => {
      const m = /^\[name="([^"]+)"\]$/.exec(sel);
      return m ? byName.get(m[1]) || null : null;
    },
    createElement: (tag) => new FakeElement(tag),
  };
}

// ---------------------------------------------------------------------------
// Page loading
// ---------------------------------------------------------------------------

// loadPage extracts the inline script from `htmlPath`, runs it against the DOM
// shim, and returns handles for driving and inspecting the page.
export async function loadPage(htmlPath, { url = 'https://app.test/_vibewarden/login', fetch }) {
  const html = readFileSync(htmlPath, 'utf8');
  const open = html.indexOf('<script>');
  const close = html.lastIndexOf('</script>');
  if (open < 0 || close < 0) throw new Error('no inline script in ' + htmlPath);
  const source = html.slice(open + '<script>'.length, close);
  if (source.includes('{{')) {
    throw new Error('inline script contains Go template actions; harness cannot run it');
  }

  const document = buildDocument(html);
  const parsed = new URL(url);
  const navigations = [];
  const location = {
    origin: parsed.origin,
    search: parsed.search,
    pathname: parsed.pathname,
    get href() {
      return parsed.href;
    },
    set href(v) {
      navigations.push(v);
    },
    reload: () => navigations.push('reload'),
  };

  const sandbox = {
    document,
    window: { location },
    history: { replaceState: () => {} },
    fetch,
    URL,
    URLSearchParams,
    console,
    setTimeout,
  };

  vm.runInNewContext(source, vm.createContext(sandbox), { filename: htmlPath });
  await flush();

  return {
    document,
    navigations,
    flush,
    el: (id) => document.getElementById(id),
    // submit fills form fields and dispatches the submit event the way a
    // browser would, with the form itself as event.target.
    submit: async (formID, { byName = {}, byID = {} } = {}) => {
      const form = document.getElementById(formID);
      if (!form) throw new Error('no form with id ' + formID);
      for (const [name, value] of Object.entries(byName)) {
        const el = document.byName.get(name) || new FakeElement('input', '');
        el.value = value;
        form[name] = el;
      }
      for (const [id, value] of Object.entries(byID)) {
        const el = document.getElementById(id);
        if (!el) throw new Error('no element with id ' + id);
        el.value = value;
      }
      await form.fire('submit', { preventDefault: () => {}, target: form });
      await flush();
    },
  };
}

// flush drains the microtask queue so awaited fetch chains inside the page
// script settle before assertions run.
export async function flush(rounds = 50) {
  for (let i = 0; i < rounds; i++) {
    await Promise.resolve();
  }
  await new Promise((resolve) => setImmediate(resolve));
  for (let i = 0; i < rounds; i++) {
    await Promise.resolve();
  }
}

// ---------------------------------------------------------------------------
// Fake Kratos
// ---------------------------------------------------------------------------

function jsonResponse(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    redirected: false,
    url: '',
    headers: { get: () => null },
    json: async () => body,
  };
}

function redirectResponse(location) {
  return {
    ok: false,
    status: 303,
    redirected: false,
    url: '',
    headers: { get: (k) => (k.toLowerCase() === 'location' ? location : null) },
    json: async () => {
      throw new Error('redirect has no JSON body');
    },
  };
}

// fakeKratos models the parts of the Ory Kratos browser API the pages touch,
// including the two behaviours that caused #1527:
//   - a flow past its lifespan answers 410 Gone with self_service_flow_expired;
//   - creating a flow rotates the anti-CSRF cookie, so tokens minted by any
//     earlier flow are rejected with security_csrf_violation.
export function fakeKratos(kind) {
  const k = {
    kind,
    flows: new Map(),
    validCSRF: '',
    seq: 0,
    gets: [],
    inits: 0,
    posts: [],
    // success builds the response for an accepted submit; overridable per test.
    success: () => jsonResponse(200, { session: { id: 'session-1' } }),
    session: {
      id: 'session-1',
      identity: { verifiable_addresses: [{ via: 'email', value: 'user@example.com', verified: true }] },
    },
  };

  k.newFlow = () => {
    k.seq += 1;
    const id = 'flow-' + k.seq;
    const csrf = 'csrf-' + k.seq;
    const flow = {
      id,
      ui: {
        action: 'https://app.test/self-service/' + kind + '?flow=' + id,
        method: 'POST',
        nodes: [{ attributes: { name: 'csrf_token', type: 'hidden', value: csrf } }],
        messages: [],
      },
    };
    k.flows.set(id, flow);
    k.validCSRF = csrf;
    return flow;
  };

  // expire drops a flow the way the lifespan sweeper does.
  k.expire = (id) => k.flows.delete(id);
  k.expireAll = () => k.flows.clear();

  k.fetch = async (rawURL, opts = {}) => {
    const u = new URL(rawURL, 'https://app.test');
    const path = u.pathname;

    if (path === '/sessions/whoami') {
      return jsonResponse(200, k.session);
    }

    if (path === '/self-service/' + kind + '/browser') {
      k.inits += 1;
      const flow = k.newFlow();
      return redirectResponse('https://app.test/_vibewarden/' + kind + '?flow=' + flow.id);
    }

    if (path === '/self-service/' + kind + '/flows') {
      const id = u.searchParams.get('id');
      k.gets.push(id);
      const flow = k.flows.get(id);
      if (!flow) {
        return jsonResponse(410, { error: { id: 'self_service_flow_expired', message: 'flow expired' } });
      }
      return jsonResponse(200, flow);
    }

    if (path === '/self-service/' + kind) {
      const flowID = u.searchParams.get('flow');
      const body = JSON.parse(opts.body || '{}');
      k.posts.push({ flowID, body });
      if (!k.flows.has(flowID)) {
        return jsonResponse(410, { error: { id: 'self_service_flow_expired', message: 'flow expired' } });
      }
      if (body.csrf_token !== k.validCSRF) {
        return jsonResponse(403, {
          error: { id: 'security_csrf_violation', message: 'the anti-CSRF cookie was found but the CSRF token was not included' },
        });
      }
      return k.success(flowID, body);
    }

    throw new Error('unexpected fetch: ' + rawURL);
  };

  return k;
}

export { jsonResponse, redirectResponse };
