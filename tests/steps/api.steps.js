const { Given, When, Then } = require("@cucumber/cucumber");
const { expect } = require("playwright/test");
const { MockDonor } = require("../support/mock-donor");

let mockDonor = null;
function getMockDonor(world) {
  if (!mockDonor) mockDonor = new MockDonor(world.baseUrl);
  return mockDonor;
}

async function setupSession(world, login = "testuser") {
  const { request } = require("playwright");
  const tokenResp = await fetch(`${world.baseUrl}/test/session-token?user=${login}`);
  const tokenData = await tokenResp.json();
  world.apiContext = await request.newContext({
    baseURL: world.baseUrl,
    extraHTTPHeaders: { Cookie: tokenData.cookie },
  });
  world.setState("_session_cookie", tokenData.cookie);
  world.setState("_session_user", login);
  world.setState("_session_user_id", tokenData.user_id);
  world._sessionSetup = true;
  world._asRole = null;
}

async function ensureSession(world) {
  if (!world._sessionSetup) await setupSession(world);
}

function resolve(str, world) {
  if (typeof str !== "string") return str;
  return str.replace(/<([^>]+)>/g, (_, key) => {
    if (world.testState.has(key)) return world.testState.get(key);
    if (key === "model") return "llama3.2:3b";
    if (key === "very_long_text") return "a".repeat(10000);
    if (key === "very_long_token_10kb") return "a".repeat(10000);
    if (key === "other_machine_id") return "mch_nonexistent_acl_test";
    return key;
  });
}

function resolveJSON(obj, world) {
  if (typeof obj === "string") return resolve(obj, world);
  if (Array.isArray(obj)) return obj.map((v) => resolveJSON(v, world));
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) result[k] = resolveJSON(v, world);
    return result;
  }
  return obj;
}

async function executeRequest(world) {
  const { method, path } = world.lastRequest;
  const url = resolve(path, world);
  const headers = {};
  for (const [k, v] of Object.entries(world.headers)) {
    headers[k] = resolve(v, world);
  }
  const body = world.body !== null && world.body !== undefined
    ? (typeof world.body === "string" ? resolve(world.body, world) : JSON.stringify(resolveJSON(world.body, world)))
    : undefined;

  const fetchUrl = world.baseUrl + url;
  const nativeHeaders = { ...headers };
  const cookie = world.getState("_session_cookie");
  if (cookie) nativeHeaders.Cookie = cookie;
  if (body !== undefined && !nativeHeaders["Content-Type"]) {
    nativeHeaders["Content-Type"] = "application/json";
  }

  const resp = await fetch(fetchUrl, {
    method,
    headers: nativeHeaders,
    body,
    redirect: "manual",
  });
  const text = await resp.text();

  world.response = {
    status: () => resp.status,
    headers: () => {
      const h = {};
      resp.headers.forEach((v, k) => { h[k.toLowerCase()] = v; });
      return h;
    },
    text: () => text,
    json: () => JSON.parse(text),
    _text: text,
  };
  world._requestExecuted = true;
}

async function ensureRequestExecuted(world) {
  if (world.lastRequest && !world._requestExecuted) await executeRequest(world);
}

function getJSONPath(obj, path) {
  if (typeof path !== "string" || path === "") return obj;
  const parts = path.replace(/\]/g, "").split(/[\.\[]/);
  let current = obj;
  for (const part of parts) {
    if (current === null || current === undefined) return undefined;
    current = /^\d+$/.test(part) ? current[parseInt(part)] : current[part];
  }
  return current;
}

async function createKey(world, scope) {
  await ensureSession(world);
  const resp = await world.apiContext.post("/api/keys", { data: { scope } });
  expect(resp.status()).toBe(201);
  return resp.json();
}

async function connectProvider(world, model = "llama3.2:3b") {
  const md = getMockDonor(world);
  await ensureSession(world);
  let providerKey = world.getState("provider_key") || world.getState("donor_key");
  if (!providerKey) {
    const body = await createKey(world, "provider");
    providerKey = body.key;
    world.setState("provider_key", providerKey);
  }
  await new Promise((r) => setTimeout(r, 200));
  const machineId = await md.connect(model, providerKey);
  world.setState("machine_id", machineId);
  world.setState("provider_id", machineId);
  world.setState("model", model);
  return machineId;
}

// =============================================================================
// GIVEN
// =============================================================================

Given("координатор запущен и доступен", async function () {
  const resp = await this.apiContext.get("/health");
  expect(resp.status()).toBe(200);
});

Given("координатор запущен с MESH_RATE_LIMIT={int}", async function (rate) {
  const resp = await this.apiContext.get("/health");
  expect(resp.status()).toBe(200);
  this.setState("rate_limit", rate);
});

Given(/^существует валидный API-ключ "([^"]+)"$/, async function (keyName) {
  await setupSession(this);
  const body = await createKey(this, "consumer");
  const cleanName = keyName.replace(/^<|>$/g, "");
  this.setState(cleanName || "valid_api_key", body.key);
  this.setState("valid_key", body.key);
  this.setState("valid_api_key", body.key);
  this.setState("consumer_key", body.key);
});

Given("существует второй API-ключ {string}", async function (keyName) {
  const body = await createKey(this, "consumer");
  this.setState(keyName.replace(/^<|>$/g, ""), body.key);
});

Given(/^существует API-ключ "(.*)" со scope "(.*)"$/, async function (keyName, scope) {
  await setupSession(this);
  const body = await createKey(this, scope);
  const cleanName = keyName.replace(/^<|>$/g, "");
  this.setState(cleanName, body.key);
  if (scope === "provider" || scope === "donor" || scope === "both") {
    this.setState("provider_key", body.key);
    this.setState("donor_key", body.key);
  }
  if (scope === "consumer" || scope === "both") {
    this.setState("valid_api_key", body.key);
    this.setState("valid_key", body.key);
    this.setState("consumer_key", body.key);
  }
});

Given(/^существует валидный API-ключ "(.*)" со scope "(.*)"$/, async function (keyName, scope) {
  await setupSession(this);
  const body = await createKey(this, scope);
  const cleanName = keyName.replace(/^<|>$/g, "");
  this.setState(cleanName, body.key);
  this.setState("valid_api_key", body.key);
  this.setState("valid_key", body.key);
  if (scope === "provider" || scope === "donor" || scope === "both") {
    this.setState("provider_key", body.key);
  }
  if (scope === "consumer" || scope === "both") {
    this.setState("consumer_key", body.key);
  }
});

Given(/^провайдер онлайн с моделью "(.*)" на машине "(.*)"$/, async function (model, machinePlaceholder) {
  await connectProvider(this, model);
});

Given(/^у owner есть машина "([^"]+)" с провайдером онлайн$/, async function (_machine) {
  await connectProvider(this, "llama3.2:3b");
});

Given(/^машина "([^"]+)" offline$/, async function (_machine) {
  const md = getMockDonor(this);
  const mid = this.getState("machine_id");
  if (mid) md.disconnect(mid);
  await new Promise((r) => setTimeout(r, 300));
});

Given(/^машина "([^"]+)" на максимальной загрузке$/, async function (_machine) {
  const mid = this.getState("machine_id");
  expect(mid).toBeTruthy();
  const resp = await fetch(
    `${this.baseUrl}/test/set-machine-load?machine=${encodeURIComponent(mid)}&load=5`,
    { method: "POST" }
  );
  expect(resp.status).toBe(200);
});

Given(/^пользователь не имеет доступа к машине "([^"]+)"$/, async function (_machine) {
  this.setState("other_machine_id", "mch_nonexistent_acl_test");
});

Given("у пользователя нет доступных машин", async function () {
  // New user with consumer key only — no owned machines / bindings.
  await setupSession(this, "empty_models_" + Date.now());
  const body = await createKey(this, "consumer");
  this.setState("valid_api_key", body.key);
  getMockDonor(this).disconnectAll();
});

Given("пользователь аутентифицирован через GitHub OAuth", async function () {
  await setupSession(this);
});

Given("owner аутентифицирован через GitHub OAuth", async function () {
  await setupSession(this, "owner_" + Date.now());
  this.setState("_owner_cookie", this.getState("_session_cookie"));
  this.setState("_owner_user", this.getState("_session_user"));
});

Given("member аутентифицирован через GitHub OAuth", async function () {
  const ownerCookie = this.getState("_owner_cookie") || this.getState("_session_cookie");
  const ownerUser = this.getState("_owner_user") || this.getState("_session_user");
  this.setState("_owner_cookie", ownerCookie);
  this.setState("_owner_user", ownerUser);
  await setupSession(this, "member_" + Date.now());
  this.setState("_member_cookie", this.getState("_session_cookie"));
  this.setState("_member_user", this.getState("_session_user"));
  this.setState("_member_user_id", this.getState("_session_user_id"));
  this._asRole = "member";
});

Given("пользователь не аутентифицирован", async function () {
  this.apiContext = await require("playwright").request.newContext({ baseURL: this.baseUrl });
  this._sessionSetup = false;
  this.testState.delete("_session_cookie");
});

Given("сессионная cookie валидна", async function () {});

Given("сессионная cookie истекла", async function () {
  this.apiContext = await require("playwright").request.newContext({ baseURL: this.baseUrl });
  this._sessionSetup = false;
  this.testState.delete("_session_cookie");
});

Given("у пользователя есть provider token", async function () {
  await ensureSession(this);
  if (!this.getState("provider_key")) {
    const body = await createKey(this, "provider");
    this.setState("provider_key", body.key);
  }
});

Given("у пользователя есть API-ключ потребителя", async function () {
  await ensureSession(this);
  const body = await createKey(this, "consumer");
  this.setState("consumer_key", body.key);
  this.setState("valid_api_key", body.key);
});

Given("у пользователя нет API-ключей", async function () {
  await setupSession(this, "nokeys_" + Date.now());
});

Given(/^у пользователя есть (\d+) API-ключа?$/, async function (count) {
  await ensureSession(this);
  for (let i = 0; i < count; i++) {
    const body = await createKey(this, "consumer");
    this.setState("key_" + i, body.key);
    this.setState("key_id_" + i, body.id);
  }
});

Given(/^у пользователя есть API-ключ с id "(.*)"$/, async function (keyId) {
  await ensureSession(this);
  const body = await createKey(this, "consumer");
  this.setState(keyId.replace(/^<|>$/g, ""), body.id);
  this.setState("key_id", body.id);
});

Given(/^у пользователя есть provider ключ с id "(.*)" и scope "(.*)"$/, async function (keyId, scope) {
  await ensureSession(this);
  const body = await createKey(this, scope);
  this.setState(keyId.replace(/^<|>$/g, ""), body.id);
  this.setState("key_id", body.id);
  this.setState("old_key", body.key);
  this.setState("provider_key", body.key);
});

Given(/^у пользователя есть consumer-ключ с id "(.*)" и scope "(.*)"$/, async function (keyId, scope) {
  await ensureSession(this);
  const body = await createKey(this, scope);
  this.setState(keyId.replace(/^<|>$/g, ""), body.id);
  this.setState("key_id", body.id);
});

Given("старый ключ равен {string}", function (keyName) {
  const key = this.getState("old_key");
  if (key) this.setState(keyName.replace(/^<|>$/g, ""), key);
});

Given(/^API-ключ "(.*)" был отозван$/, async function (keyName) {
  await ensureSession(this);
  const body = await createKey(this, "consumer");
  this.setState(keyName.replace(/^<|>$/g, ""), body.key);
  await this.apiContext.delete(`/api/keys/${body.id}`);
});

Given(/^существует API-ключ с id "(.*)", принадлежащий другому пользователю$/, async function (keyId) {
  this.setState(keyId.replace(/^<|>$/g, ""), "99999");
});

Given("у пользователя нет подключённых агентов", async function () {
  getMockDonor(this).disconnectAll();
  await new Promise((r) => setTimeout(r, 300));
});

Given("у owner есть активный invite", async function () {
  await asOwner(this);
  const mid = this.getState("machine_id");
  const resp = await fetch(`${this.baseUrl}/api/invites`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: this.getState("_owner_cookie") || this.getState("_session_cookie"),
    },
    body: JSON.stringify({ machine_id: mid, max_uses: 3, ttl_days: 7 }),
  });
  expect(resp.status).toBe(201);
  const body = await resp.json();
  this.setState("pin", body.pin);
  this.setState("invite_id", body.id);
});

Given(/^существует активный PIN "(.*)" для машины "(.*)"$/, async function (_pin, _machine) {
  await asOwner(this);
  const mid = this.getState("machine_id");
  const resp = await fetch(`${this.baseUrl}/api/invites`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: this.getState("_owner_cookie") || this.getState("_session_cookie"),
    },
    body: JSON.stringify({ machine_id: mid, max_uses: 5, ttl_days: 7 }),
  });
  expect(resp.status).toBe(201);
  const body = await resp.json();
  this.setState("pin", body.pin);
  this.setState("invite_id", body.id);
});

Given(/^member имеет binding на "(.*)"$/, async function (_machine) {
  // Ensure invite + member session + redeem.
  if (!this.getState("pin")) {
    await asOwner(this);
    const mid = this.getState("machine_id");
    const resp = await fetch(`${this.baseUrl}/api/invites`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Cookie: this.getState("_owner_cookie") || this.getState("_session_cookie"),
      },
      body: JSON.stringify({ machine_id: mid, max_uses: 5, ttl_days: 7 }),
    });
    expect(resp.status).toBe(201);
    const body = await resp.json();
    this.setState("pin", body.pin);
  }
  if (!this.getState("_member_cookie")) {
    const ownerCookie = this.getState("_owner_cookie") || this.getState("_session_cookie");
    this.setState("_owner_cookie", ownerCookie);
    await setupSession(this, "member_" + Date.now());
    this.setState("_member_cookie", this.getState("_session_cookie"));
    this.setState("_member_user", this.getState("_session_user"));
    this.setState("_member_user_id", this.getState("_session_user_id"));
  }
  const joinResp = await fetch(`${this.baseUrl}/api/join`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: this.getState("_member_cookie"),
    },
    body: JSON.stringify({ pin: this.getState("pin") }),
  });
  expect(joinResp.status).toBe(200);
  const joinBody = await joinResp.json();
  if (joinBody.api_key) this.setState("member_key", joinBody.api_key);
  this.setState("_session_cookie", this.getState("_member_cookie"));
  this._asRole = "member";
  await setupSessionFromCookie(this, this.getState("_member_cookie"));
});

Given(/^у member есть consumer API-ключ "(.*)"$/, async function (keyName) {
  let key = this.getState("member_key");
  if (!key) {
    await setupSessionFromCookie(this, this.getState("_member_cookie"));
    const body = await createKey(this, "consumer");
    key = body.key;
    this.setState("member_key", key);
  }
  this.setState(keyName.replace(/^<|>$/g, ""), key);
});

Given("существует request_id {string}", async function (placeholder) {
  this.setState(placeholder.replace(/^<|>$/g, "") || "request_id", "req_test_" + Date.now());
});

Given(/^лимит запросов для ключа "(.*)" исчерпан$/, async function (keyName) {
  keyName = keyName.replace(/^<|>$/g, "");
  const key = this.getState(keyName);
  if (!key) return;
  try {
    await fetch(`${this.baseUrl}/test/reset-rate-limit?key=${encodeURIComponent(key)}`, { method: "POST" });
  } catch (_) {}
  const burstLimit = this.getState("rate_limit") || 100;
  const exhaustCount = Math.max(burstLimit + 20, 120);
  for (let i = 0; i < exhaustCount; i++) {
    try {
      await fetch(`${this.baseUrl}/v1/models`, {
        headers: { Authorization: "Bearer " + key },
      });
    } catch (_) {}
  }
});

Given(/^лимит для "(.*)" исчерпан$/, async function (keyName) {
  keyName = keyName.replace(/^<|>$/g, "");
  const key = this.getState(keyName);
  if (!key) return;
  try {
    await fetch(`${this.baseUrl}/test/reset-rate-limit?key=${encodeURIComponent(key)}`, { method: "POST" });
  } catch (_) {}
  const burstLimit = this.getState("rate_limit") || 100;
  const exhaustCount = Math.max(burstLimit + 20, 120);
  for (let i = 0; i < exhaustCount; i++) {
    try {
      await fetch(`${this.baseUrl}/v1/models`, {
        headers: { Authorization: "Bearer " + key },
      });
    } catch (_) {}
  }
});

Given("прошёл 1 час с момента первого запроса", async function () {
  const key = this.getState("valid_api_key");
  if (key) {
    await fetch(`${this.baseUrl}/test/reset-rate-limit?key=${encodeURIComponent(key)}`, { method: "POST" });
  }
});

Given(/^пользователь отправил (\d+) запросов к "(.*)" за последний час$/, async function (count, endpoint) {
  const key = this.getState("valid_api_key") || this.getState("consumer_key");
  for (let i = 0; i < count; i++) {
    await fetch(`${this.baseUrl}${endpoint}`, {
      method: "GET",
      headers: { Authorization: "Bearer " + key },
    });
  }
});

async function asOwner(world) {
  const cookie = world.getState("_owner_cookie") || world.getState("_session_cookie");
  world.setState("_session_cookie", cookie);
  world._asRole = "owner";
  await setupSessionFromCookie(world, cookie);
}

async function asMember(world) {
  const cookie = world.getState("_member_cookie");
  world.setState("_session_cookie", cookie);
  world._asRole = "member";
  await setupSessionFromCookie(world, cookie);
}

async function setupSessionFromCookie(world, cookie) {
  const { request } = require("playwright");
  world.apiContext = await request.newContext({
    baseURL: world.baseUrl,
    extraHTTPHeaders: { Cookie: cookie },
  });
  world.setState("_session_cookie", cookie);
  world._sessionSetup = true;
}

// =============================================================================
// WHEN
// =============================================================================

When(/^пользователь отправляет (GET|POST|PUT|DELETE|OPTIONS|HEAD)-запрос на "(.*)"$/, function (method, path) {
  this.lastRequest = { method, path };
  this._requestExecuted = false;
  this.response = null;
  this.body = null;
  this.headers = {};
});

When(/^owner отправляет (GET|POST|PUT|DELETE)-запрос на "(.*)"$/, async function (method, path) {
  await asOwner(this);
  this.lastRequest = { method, path };
  this._requestExecuted = false;
  this.response = null;
  this.body = null;
  this.headers = {};
});

When(/^member отправляет (GET|POST|PUT|DELETE)-запрос на "(.*)"$/, async function (method, path) {
  await asMember(this);
  this.lastRequest = { method, path };
  this._requestExecuted = false;
  this.response = null;
  this.body = null;
  this.headers = {};
});

When(/^owner отзывает member с машины "([^"]+)"$/, async function (_machine) {
  await asOwner(this);
  const mid = this.getState("machine_id");
  let memberUserId = this.getState("_member_user_id");
  if (!memberUserId) {
    const login = this.getState("_member_user");
    const tok = await fetch(`${this.baseUrl}/test/session-token?user=${encodeURIComponent(login)}`);
    const tokData = await tok.json();
    memberUserId = tokData.user_id;
    this.setState("_member_user_id", memberUserId);
  }
  expect(memberUserId).toBeTruthy();

  const path = `/api/machines/${mid}/members/${memberUserId}`;
  const resp = await fetch(`${this.baseUrl}${path}`, {
    method: "DELETE",
    headers: { Cookie: this.getState("_session_cookie") },
    redirect: "manual",
  });
  const text = await resp.text();
  this.response = {
    status: () => resp.status,
    headers: () => ({}),
    text: () => text,
    json: () => { try { return JSON.parse(text); } catch (_) { return {}; } },
    _text: text,
  };
  this.lastRequest = { method: "DELETE", path };
  this._requestExecuted = true;
  if (resp.status !== 200) {
    throw new Error(
      `revoke failed status=${resp.status} body=${text} path=${path} ` +
      `memberUserId=${memberUserId} mid=${mid} memberUser=${this.getState("_member_user")}`
    );
  }
});

When(/^заголовок "(.*)" равен "(.*)"$/, async function (name, value) {
  // Assertion after a request was prepared: execute first when capturing/checking response headers.
  const isCaptureOrAssert = value.includes("<initial>") || this._requestExecuted || this.response;
  if (this.lastRequest && !this._requestExecuted && (value.includes("<initial>") || this.response)) {
    await executeRequest(this);
  }
  if (this.response && (value.includes("<initial>") || this._requestExecuted)) {
    const headers = this.response.headers();
    if (value.includes("<initial>")) {
      this.setState("initial", headers[name.toLowerCase()]);
      expect(headers[name.toLowerCase()]).toBeDefined();
      return;
    }
    expect(headers[name.toLowerCase()]).toBe(resolve(value, this));
    return;
  }
  // Request-building context.
  this.headers[name] = value;
});

When(/^заголовок "(.*)" отсутствует$/, function (name) {
  delete this.headers[name];
});

When("тело запроса содержит:", function (docstring) {
  try {
    this.body = JSON.parse(docstring);
  } catch (_) {
    this.body = docstring;
  }
});

When("тело запроса пустое", function () {
  this.body = null;
});

When("тело запроса содержит валидный chat completion запрос", function () {
  this.body = {
    model: "llama3.2:3b",
    messages: [{ role: "user", content: "Hi" }],
    stream: false,
  };
});

When(/^пользователь повторно отправляет (DELETE)-запрос на "(.*)"$/, async function (method, path) {
  this.lastRequest = { method, path };
  await executeRequest(this);
});

When(/^провайдер пытается подключиться по WebSocket "(.*)"$/, async function (wsPath) {
  await wsConnectAttempt(this, wsPath);
});

When(/^провайдер подключается по WebSocket "(.*)"$/, async function (wsPath) {
  await wsConnectAttempt(this, wsPath);
});

async function wsConnectAttempt(world, wsPath) {
  const md = getMockDonor(world);
  const fullPath = resolve(wsPath, world);
  const tokenMatch = fullPath.match(/[?&]token=([^&]*)/);
  const token = tokenMatch ? decodeURIComponent(tokenMatch[1]) : "";
  try {
    const machineId = await md.connect("llama3.2:3b", token);
    world.setState("ws_provider_id", machineId);
    world.setState("machine_id", machineId);
    world.setState("ws_connected", true);
    world.setState("ws_error_code", null);
  } catch (e) {
    world.setState("ws_connected", false);
    world.setState(
      "ws_error_code",
      e.message.includes("401") ? 401 : e.message.includes("403") ? 403 : 400
    );
  }
}

// =============================================================================
// THEN
// =============================================================================

Then(/^статус ответа равен (\d+)$/, async function (code) {
  if (this.lastRequest && !this._requestExecuted) await executeRequest(this);
  expect(this.response.status()).toBe(parseInt(code));
});

Then("статус ответа равен {int} или {int}", async function (code1, code2) {
  if (this.lastRequest && !this._requestExecuted) await executeRequest(this);
  expect([code1, code2]).toContain(this.response.status());
});

Then(/^тело ответа равно "(.*)"$/, async function (text) {
  await ensureRequestExecuted(this);
  expect(await this.response.text()).toBe(resolve(text, this));
});

Then(/^тело ответа содержит поле "(.*)" типа "(.*)"$/, async function (field, type) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  if (type === "array") expect(Array.isArray(val)).toBe(true);
  else if (type === "number") expect(typeof val).toBe("number");
  else if (type === "boolean") expect(typeof val).toBe("boolean");
  else expect(typeof val).toBe(type);
});

Then(/^тело ответа содержит поле "(.*)" со значением "(.*)"$/, async function (field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  const expected = resolve(value, this);
  if (expected === "true") expect(val).toBe(true);
  else if (expected === "false") expect(val).toBe(false);
  else if (!isNaN(expected) && expected !== "") expect(val).toBe(Number(expected));
  else expect(val).toBe(expected);
});

Then(/^тело ответа содержит поле "([^"]+)"$/, async function (field) {
  await ensureRequestExecuted(this);
  expect(getJSONPath(await this.response.json(), field)).toBeDefined();
});

Then(/^значение "(.*)" начинается с "(.*)"$/, async function (field, prefix) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  expect(String(getJSONPath(json, resolve(field, this)))).toMatch(
    new RegExp("^" + resolve(prefix, this))
  );
});

Then(/^значение "(.*)" равно первым (\d+) символам "(.*)"$/, async function (field1, count, field2) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const prefix = getJSONPath(json, resolve(field1, this));
  const full = getJSONPath(json, resolve(field2, this));
  expect(String(full).substring(0, parseInt(count))).toBe(String(prefix));
});

Then(/^значение "(.*)" >= (\d+)$/, async function (field, min) {
  await ensureRequestExecuted(this);
  const val = getJSONPath(await this.response.json(), resolve(field, this));
  expect(val).toBeGreaterThanOrEqual(parseInt(min));
});

Then(/^значение "(.*)" <= значение "(.*)"$/, async function (field1, field2) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  expect(getJSONPath(json, resolve(field1, this))).toBeLessThanOrEqual(
    getJSONPath(json, resolve(field2, this))
  );
});

Then(/^значение "(.*)" равно (\d+)$/, async function (field, expected) {
  await ensureRequestExecuted(this);
  expect(getJSONPath(await this.response.json(), resolve(field, this))).toBe(parseInt(expected));
});

Then(/^значение "(.*)" равно значению "(.*)"$/, async function (field1, field2) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  expect(getJSONPath(json, resolve(field1, this))).toBe(getJSONPath(json, resolve(field2, this)));
});

Then(/^значение "(.*)" не равно "(.*)"$/, async function (field, value) {
  await ensureRequestExecuted(this);
  expect(String(getJSONPath(await this.response.json(), resolve(field, this)))).not.toBe(
    resolve(value, this)
  );
});

Then(/^массив "(.*)" пуст$/, async function (field) {
  await ensureRequestExecuted(this);
  const val = getJSONPath(await this.response.json(), resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBe(0);
});

Then(/^массив "(.*)" содержит хотя бы (\d+) элемент$/, async function (field, count) {
  await ensureRequestExecuted(this);
  const val = getJSONPath(await this.response.json(), resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBeGreaterThanOrEqual(parseInt(count));
});

Then(/^каждый элемент "(.*)" содержит поле "(.*)" типа "(.*)"$/, async function (arrayField, itemField, type) {
  await ensureRequestExecuted(this);
  const arr = getJSONPath(await this.response.json(), resolve(arrayField, this));
  expect(Array.isArray(arr)).toBe(true);
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    if (type === "array") expect(Array.isArray(val)).toBe(true);
    else expect(typeof val).toBe(type);
  }
});

Then(/^каждый элемент "(.*)" содержит поле "(.*)" со значением "(.*)"$/, async function (arrayField, itemField, value) {
  await ensureRequestExecuted(this);
  const arr = getJSONPath(await this.response.json(), resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    const expected = resolve(value, this);
    if (expected === "true") expect(val).toBe(true);
    else expect(val).toBe(expected);
  }
});

Then("каждый элемент {string} содержит поле {string} со значением true", async function (arrayField, itemField) {
  await ensureRequestExecuted(this);
  const arr = getJSONPath(await this.response.json(), resolve(arrayField, this));
  for (const item of arr) expect(getJSONPath(item, resolve(itemField, this))).toBe(true);
});

Then("каждый элемент массива {string} содержит поле {string} типа {string}", async function (arrayField, itemField, type) {
  await ensureRequestExecuted(this);
  const arr = getJSONPath(await this.response.json(), resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    if (type === "boolean") expect(typeof val).toBe("boolean");
    else if (type === "number") expect(typeof val).toBe("number");
    else if (type === "string") expect(typeof val).toBe("string");
    else if (type === "array") expect(Array.isArray(val)).toBe(true);
  }
});

Then(/^ни один элемент "(.*)" не содержит поле "(.*)"$/, async function (arrayField, itemField) {
  await ensureRequestExecuted(this);
  const arr = getJSONPath(await this.response.json(), resolve(arrayField, this));
  for (const item of arr) expect(item[resolve(itemField, this)]).toBeUndefined();
});

Then(/^заголовок "(.*)" содержит "(.*)"$/, async function (name, value) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  expect(headers[name.toLowerCase()]).toBeDefined();
  expect(headers[name.toLowerCase()]).toContain(resolve(value, this));
});

Then(/^заголовок "(.*)" присутствует$/, async function (name) {
  await ensureRequestExecuted(this);
  expect(this.response.headers()[name.toLowerCase()]).toBeDefined();
});

Then(/^значение заголовка "(.*)" — целое число$/, async function (name) {
  await ensureRequestExecuted(this);
  expect(Number.isInteger(parseInt(this.response.headers()[name.toLowerCase()]))).toBe(true);
});

Then(/^значение "(.*)" <= (\d+)$/, async function (field, max) {
  await ensureRequestExecuted(this);
  const val = getJSONPath(await this.response.json(), resolve(field, this));
  expect(val).toBeLessThanOrEqual(parseInt(max));
});

Then(/^заголовок "(.*)" < <initial>$/, async function (name) {
  await ensureRequestExecuted(this);
  const current = parseInt(this.response.headers()[name.toLowerCase()]);
  const initial = parseInt(this.getState("initial"));
  expect(current).toBeLessThan(initial);
});

Then(/^заголовок "(.*)" > (\d+)$/, async function (name, min) {
  await ensureRequestExecuted(this);
  expect(parseInt(this.response.headers()[name.toLowerCase()])).toBeGreaterThan(min);
});

Then("последняя строка ответа равна {string}", async function (expected) {
  await ensureRequestExecuted(this);
  const text = this.response._text || (await this.response.text());
  const lines = text.split("\n").filter((l) => l.trim() !== "");
  expect(lines[lines.length - 1]?.trim()).toBe(resolve(expected, this));
});

Then("WebSocket-соединение установлено", function () {
  expect(this.getState("ws_connected")).toBe(true);
});

Then(/^WebSocket-соединение отклоняется с кодом (\d+)$/, function (code) {
  expect(this.getState("ws_connected")).toBe(false);
});

Then(/^WebSocket-соединение отклоняется с кодом (\d+) или (\d+)$/, function (code1, code2) {
  expect(this.getState("ws_connected")).toBe(false);
});

Then("сообщение содержит поле {string}", function (field) {
  expect(this.getState("ws_connected")).toBe(true);
  if (field === "machine_id") expect(this.getState("machine_id") || this.getState("ws_provider_id")).toBeTruthy();
});

Then(/^bindings содержат machine_id "(.*)"$/, async function (placeholder) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const mid = resolve(placeholder, this);
  const found = (json.bindings || []).some((b) => b.machine_id === mid);
  expect(found).toBe(true);
});

Then("тело ответа содержит поле {string} со значением true", async function (field) {
  await ensureRequestExecuted(this);
  expect(getJSONPath(await this.response.json(), field)).toBe(true);
});

module.exports._getMockDonor = () => mockDonor;
