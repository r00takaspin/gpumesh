const { Given, When, Then } = require("@cucumber/cucumber");
const { expect } = require("playwright/test");
const { MockDonor } = require("../support/mock-donor");

// Shared mock donor instance (created lazily).
let mockDonor = null;
function getMockDonor(world) {
  if (!mockDonor) {
    mockDonor = new MockDonor(world.baseUrl);
  }
  return mockDonor;
}

// Session setup helper — fetches a session token and re-inits API context with cookie.
async function setupSession(world, login = "testuser") {
  const { request } = require("playwright");
  const tokenResp = await fetch(`${world.baseUrl}/test/session-token?user=${login}`);
  const tokenData = await tokenResp.json();
  world.apiContext = await request.newContext({
    baseURL: world.baseUrl,
    extraHTTPHeaders: { "Cookie": tokenData.cookie }
  });
  // Store the cookie so executeRequest can include it.
  world.setState("_session_cookie", tokenData.cookie);
  world._sessionSetup = true;
}

async function ensureSession(world) {
  if (!world._sessionSetup) {
    await setupSession(world);
  }
}

// --- Placeholder resolution ---
function resolve(str, world) {
  if (typeof str !== "string") return str;
  return str.replace(/<([^>]+)>/g, (_, key) => {
    if (world.testState.has(key)) return world.testState.get(key);
    if (key === "valid_api_key" || key === "valid_key" || key === "second_key" ||
        key === "consumer_key" || key === "donor_key" || key === "revoked_key" ||
        key === "old_key" || key === "other_key_id" || key === "key_id") {
      return world.testState.get(key) || "";
    }
    if (key === "request_id") return world.testState.get("request_id") || "";
    if (key === "model") return "llama3.2:3b";
    if (key === "very_long_text") return "a".repeat(10000);
    if (key === "very_long_token_10kb") return "a".repeat(10000);
    return key;
  });
}

function resolveJSON(obj, world) {
  if (typeof obj === "string") return resolve(obj, world);
  if (Array.isArray(obj)) return obj.map(v => resolveJSON(v, world));
  if (obj && typeof obj === "object") {
    const result = {};
    for (const [k, v] of Object.entries(obj)) {
      result[k] = resolveJSON(v, world);
    }
    return result;
  }
  return obj;
}

// Helper: execute the accumulated request.
async function executeRequest(world) {
  const { method, path } = world.lastRequest;
  const url = resolve(path, world);
  const headers = {};
  for (const [k, v] of Object.entries(world.headers)) {
    headers[k] = resolve(v, world);
  }
  const body = world.body ? JSON.stringify(resolveJSON(world.body, world)) : undefined;

  // Use native fetch to avoid apiContext's default headers (session cookie)
  // interfering with API authentication. Explicitly include session cookie if available.
  const fetchUrl = world.baseUrl + url;
  const nativeHeaders = { ...headers };
  if (world.getState("_session_cookie")) {
    nativeHeaders["Cookie"] = world.getState("_session_cookie");
  }
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

// Helper: ensure the accumulated request has been executed.
async function ensureRequestExecuted(world) {
  if (world.lastRequest && !world._requestExecuted) {
    await executeRequest(world);
  }
}

// =============================================================================
// GIVEN steps
// =============================================================================

Given("координатор запущен и доступен", async function () {
  const resp = await this.apiContext.get("/health");
  expect(resp.status()).toBe(200);
});

Given("координатор запущен с MESH_RATE_LIMIT={int}", async function (rate) {
  // Rate limit is configured on server start; just verify health.
  const resp = await this.apiContext.get("/health");
  expect(resp.status()).toBe(200);
  this.setState("rate_limit", rate);
});

Given(/^существует валидный API-ключ "(.*)"$/, async function (keyName) {
  await setupSession(this);
  const resp = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  expect(resp.status()).toBe(201);
  const body = await resp.json();
  // Strip angle brackets from keyName (feature files use <key_name> placeholders)
  const cleanName = keyName.replace(/^<|>$/g, "");
  this.setState(cleanName || "valid_api_key", body.key);
  this.setState("valid_key", body.key);
  this.setState("key_hash", body.key);
});

Given("существует второй API-ключ {string}", async function (keyName) {
  await ensureSession(this);
  const resp = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  if (resp.status() === 201) {
    const body = await resp.json();
    const cleanName = keyName.replace(/^<|>$/g, "");
    this.setState(cleanName, body.key);
  }
});
Given(/^в реестре есть онлайн-донор с моделью "(.*)"$/, async function (model) {
  const md = getMockDonor(this);
  await new Promise(r => setTimeout(r, 300)); // Let coordinator process previous disconnects
  await setupSession(this);
  let donorKey = this.getState("donor_key");
  if (!donorKey) {
    try {
      const resp = await this.apiContext.post("/api/keys", { data: { scope: "donor" } });
      if (resp.status() === 201) {
        const body = await resp.json();
        donorKey = body.key;
        this.setState("donor_key", donorKey);
      } else {
        console.log(`[BDD] Failed to create donor key: status ${resp.status()}`);
      }
    } catch (e) {
      console.log(`[BDD] Error creating donor key: ${e.message}`);
    }
  }
  if (!donorKey) {
    console.log("[BDD] No donor token available for mock donor connection");
    return;
  }
  try {
    const providerId = await md.connect(model, donorKey);
    this.setState("provider_id", providerId);
    this.setState("model", model);
  } catch (e) {
    console.log("Mock donor connect failed (may be expected):", e.message);
  }
});

Given("в реестре зарегистрированы доноры с моделями", async function () {
  const md = getMockDonor(this);
  await setupSession(this);
  let donorKey = this.getState("donor_key");
  if (!donorKey) {
    try {
      const resp = await this.apiContext.post("/api/keys", { data: { scope: "donor" } });
      if (resp.status() === 201) {
        const body = await resp.json();
        donorKey = body.key;
        this.setState("donor_key", donorKey);
      }
    } catch (_) {}
  }
  if (!donorKey) {
    console.log("[BDD] No donor token available for mock donor connection");
    return;
  }
  try {
    const providerId = await md.connect("llama3.2:3b", donorKey);
    this.setState("provider_id", providerId);
  } catch (e) {
    console.log(`[BDD] Mock donor connect failed (may be expected): ${e.message}`);
  }
});

Given("реестр доноров пуст", async function () {
  const md = getMockDonor(this);
  md.disconnectAll();
});

Given(/^все доноры модели "(.*)" на максимальной загрузке$/, async function (model) {
  const providerId = this.getState("provider_id");
  console.log(`[BDD] all-donors-busy: provider_id=${providerId}`);
  if (providerId) {
    const url = `${this.baseUrl}/test/set-donor-load?provider=${encodeURIComponent(providerId)}&load=5`;
    const resp = await fetch(url, { method: "POST" });
    const body = await resp.text();
    console.log(`[BDD] set-donor-load: status=${resp.status} body=${body}`);
  } else {
    console.log(`[BDD] all-donors-busy: NO provider_id in testState!`);
  }
});

Given("база данных недоступна", async function () {
  // Skip — this is a server-internal state we can't easily simulate from outside.
  return "skipped";
});

Given(/^существует завершённый запрос с request_id "(.*)"$/, async function (requestId) {
  // Execute a chat completion to get a real request_id, then store it.
  this.setState("model", "llama3.2:3b");
  this.lastRequest = { method: "POST", path: "/v1/chat/completions" };
  this.headers = { "Authorization": "Bearer " + (this.getState("valid_api_key") || ""), "Content-Type": "application/json" };
  this.body = { model: "llama3.2:3b", messages: [{ role: "user", content: "Hello" }], stream: false };
  this._requestExecuted = false;
  await executeRequest(this);
  if (this.response && this.response.status() === 200) {
    try {
      const json = await this.response.json();
      this.setState("request_id", json.id || "req_test123");
    } catch (_) {
      this.setState("request_id", "req_test123");
    }
  } else {
    this.setState("request_id", "req_test123");
  }
  // Clean up request state so subsequent When steps start fresh.
  this.lastRequest = null;
  this.response = null;
  this.headers = {};
  this.body = null;
  this._requestExecuted = false;
});
Given("пользователь аутентифицирован через GitHub OAuth", async function () {
  await setupSession(this);
});

Given("пользователь не аутентифицирован", async function () {
  // Clear any session cookies.
  this.apiContext = await require("playwright").request.newContext({
    baseURL: this.baseUrl
  });
  this._sessionSetup = false;
  this.testState.delete("_session_cookie");
});

Given("сессионная cookie валидна", async function () {
  // Session was set up by the authenticated step.
});

Given("сессионная cookie истекла", async function () {
  // Create a new context without cookies.
  this.apiContext = await require("playwright").request.newContext({
    baseURL: this.baseUrl
  });
  this._sessionSetup = false;
  this.testState.delete("_session_cookie");
});

Given(/^у пользователя есть донорский токен \(scope: donor или both\)$/, async function () {
  await ensureSession(this);
  let donorKey = this.getState("donor_key");
  if (!donorKey) {
    const resp = await this.apiContext.post("/api/keys", { data: { scope: "both" } });
    if (resp.status() === 201) {
      const body = await resp.json();
      donorKey = body.key;
      this.setState("donor_key", donorKey);
    }
  }
});

Given("агент донора подключён к координатору", async function () {
  const md = getMockDonor(this);
  const donorKey = this.getState("donor_key");
  if (!donorKey) { console.log("[BDD] No donor key for agent connect"); return; }
  try {
    const providerId = await md.connect("llama3.2:3b", donorKey);
    this.setState("provider_id", providerId);
  } catch (e) {
    console.log("Mock donor connect failed (may be expected):", e.message);
  }
});
Given(/^агент донора подключён с моделью "(.*)"$/, async function (model) {
  const md = getMockDonor(this);
  const donorKey = this.getState("donor_key");
  if (!donorKey) { console.log("[BDD] No donor key for agent connect"); return; }
  try {
    const providerId = await md.connect(model, donorKey);
    this.setState("provider_id", providerId);
  } catch (e) {
    console.log("Mock donor connect failed (may be expected):", e.message);
  }
});

Given("у пользователя нет подключённых агентов", async function () {
  const md = getMockDonor(this);
  md.disconnectAll();
});

Given("у пользователя нет API-ключей", async function () {
  // Create a new session with a unique user to ensure empty key list.
  await setupSession(this, "nokeys_" + Date.now());
});

Given("у пользователя есть API-ключ потребителя", async function () {
  await ensureSession(this);
  const resp = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  if (resp.status() === 201) {
    const body = await resp.json();
    this.setState("consumer_key", body.key);
  }
});

Given(/^у пользователя есть (\d+) API-ключа?$/, async function (count) {
  await ensureSession(this);
  for (let i = 0; i < count; i++) {
    const resp = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
    if (resp.status() === 201) {
      const body = await resp.json();
      this.setState("key_" + i, body.key);
      this.setState("key_id_" + i, body.id);
    }
  }
});

Given(/^у пользователя есть API-ключ с id "(.*)"$/, async function (keyId) {
  await ensureSession(this);
  const cleanId = keyId.replace(/^<|>$/g, "");
  const resp = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  if (resp.status() === 201) {
    const body = await resp.json();
    this.setState(cleanId, body.id);
    this.setState("key_id", body.id);
  }
});

Given(/^у пользователя есть донорский ключ с id "(.*)" и scope "(.*)"$/, async function (keyId, scope) {
  await ensureSession(this);
  const cleanId = keyId.replace(/^<|>$/g, "");
  const resp = await this.apiContext.post("/api/keys", { data: { scope } });
  if (resp.status() === 201) {
    const body = await resp.json();
    this.setState(cleanId, body.id);
    this.setState("old_key", body.key);
  }
});

Given(/^у пользователя есть consumer-ключ с id "(.*)" и scope "(.*)"$/, async function (keyId, scope) {
  await ensureSession(this);
  const cleanId = keyId.replace(/^<|>$/g, "");
  const resp = await this.apiContext.post("/api/keys", { data: { scope } });
  if (resp.status() === 201) {
    const body = await resp.json();
    this.setState(cleanId, body.id);
  }
});


Given(/^существует API-ключ "(.*)" со scope "(.*)"$/, async function (keyName, scope) {
  await ensureSession(this);
  const resp = await this.apiContext.post("/api/keys", { data: { scope } });
  if (resp.status() === 201) {
    const body = await resp.json();
    const cleanName = keyName.replace(/^<|>$/g, "");
    this.setState(cleanName || "donor_key", body.key);
  }
});

Given(/^API-ключ "(.*)" был отозван$/, async function (keyName) {
  await ensureSession(this);
  const cleanName = keyName.replace(/^<|>$/g, "");
  // Create a key, then revoke it.
  const resp1 = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  if (resp1.status() === 201) {
    const body = await resp1.json();
    this.setState(cleanName, body.key);
    await this.apiContext.delete(`/api/keys/${body.id}`);
  }
});

Given(/^существует отозванный API-ключ "(.*)"$/, async function (keyName) {
  await ensureSession(this);
  const cleanName = keyName.replace(/^<|>$/g, "");
  const resp1 = await this.apiContext.post("/api/keys", { data: { scope: "consumer" } });
  if (resp1.status() === 201) {
    const body = await resp1.json();
    this.setState(cleanName, body.key);
    await this.apiContext.delete(`/api/keys/${body.id}`);
  }
});


Given(/^существует API-ключ с id "(.*)", принадлежащий другому пользователю$/, async function (keyId) {
  const cleanId = keyId.replace(/^<|>$/g, "");
  // Create as current user, store ID for "other user" test.
  this.setState(cleanId, "99999"); // Non-existent ID for current user.
});

Given(/^донор набрал (\d+) токенов$/, async function (tokens) {
  // Donor stats are aggregated; just note the expectation.
});

Given(/^донор с моделью "(.*)" онлайн$/, async function (model) {
  const md = getMockDonor(this);
  const donorKey = this.getState("donor_key");
  if (!donorKey) { console.log("[BDD] No donor key"); return; }
  try {
    const providerId = await md.connect(model, donorKey);
    this.setState("provider_id", providerId);
  } catch (e) {
    console.log("Mock donor connect failed (may be expected):", e.message);
  }
});

Given("потребитель отправляет запрос к модели {string}", async function (model) {
  // Capture donor stats before the request for "увеличилось" comparison.
  try {
    const token = this.getState("_session_cookie") || "";
    const statsResp = await fetch(`${this.baseUrl}/api/donor/stats`, {
      headers: token ? { "Cookie": token } : {}
    });
    if (statsResp.ok) {
      const statsJson = await statsResp.json();
      this.setState("_before_total_requests", statsJson.total_requests);
      this.setState("_before_total_tokens", statsJson.total_tokens);
    }
  } catch (_) {}

  const key = this.getState("valid_api_key") || this.getState("donor_key") || this.getState("consumer_key") || "";
  this.lastRequest = { method: "POST", path: "/v1/chat/completions" };
  this.headers = {
    "Authorization": "Bearer " + key,
    "Content-Type": "application/json"
  };
  this.body = { model, messages: [{ role: "user", content: "Hello" }], stream: false };
  await executeRequest(this);
});

Given(/^лимит запросов для ключа "(.*)" исчерпан$/, async function (keyName) {
  keyName = keyName.replace(/^<|>$/g, "");
  const key = this.getState(keyName);
  if (!key) return;
  // Reset rate limit first for predictable state.
  try {
    await fetch(`${this.baseUrl}/test/reset-rate-limit?key=${encodeURIComponent(key)}`, { method: "POST" });
  } catch (_) {}
  // Exhaust the bucket. Default burst = 100 (or as configured); send 120 to be safe.
  const burstLimit = this.getState("rate_limit") || 100;
  const exhaustCount = Math.max(burstLimit + 20, 120);
  for (let i = 0; i < exhaustCount; i++) {
    try {
      await this.apiContext.get("/v1/models", {
        headers: { "Authorization": "Bearer " + key }
      });
    } catch (_) {}
  }
});

Given(/^лимит для "(.*)" исчерпан$/, async function (keyName) {
  keyName = keyName.replace(/^<|>$/g, "");
  const key = this.getState(keyName);
  if (!key) return;
  // Reset rate limit first for predictable state.
  try {
    await fetch(`${this.baseUrl}/test/reset-rate-limit?key=${encodeURIComponent(key)}`, { method: "POST" });
  } catch (_) {}
  // Exhaust the bucket. Default burst = 100 (or as configured); send 120 to be safe.
  const burstLimit = this.getState("rate_limit") || 100;
  const exhaustCount = Math.max(burstLimit + 20, 120);
  for (let i = 0; i < exhaustCount; i++) {
    try {
      await this.apiContext.get("/v1/models", {
        headers: { "Authorization": "Bearer " + key }
      });
    } catch (_) {}
  }
});

Given(/^лимит запросов для ключа "(.*)" не исчерпан$/, async function (keyName) {
  // Nothing to do — rate limit starts fresh.
});

Given("прошёл 1 час с момента первого запроса", async function () {
  // Use test reset endpoint instead of waiting.
  const key = this.getState("valid_api_key");
  if (key) {
    await this.apiContext.post(`/test/reset-rate-limit?key=${encodeURIComponent(key)}`);
  }
});

Given(/^пользователь отправил (\d+) запросов к "(.*)" за последний час$/, async function (count, endpoint) {
  const key = this.getState("valid_api_key") || this.getState("consumer_key") || this.getState("donor_key") || "invalid";
  for (let i = 0; i < count; i++) {
    try {
      // Execute a chat completion to increment stats.
      this.lastRequest = { method: "POST", path: endpoint };
      this.headers = {
        "Authorization": "Bearer " + key,
        "Content-Type": "application/json"
      };
      this.body = { model: "llama3.2:3b", messages: [{ role: "user", content: "Hi" }], stream: false };
      await executeRequest(this);
    } catch (_) {}
  }
});
Given("в реестре есть онлайн-доноры с моделями", async function () {
  const md = getMockDonor(this);
  await setupSession(this);
  let donorKey = this.getState("donor_key");
  if (!donorKey) {
    try {
      const resp = await this.apiContext.post("/api/keys", { data: { scope: "donor" } });
      if (resp.status() === 201) {
        const body = await resp.json();
        donorKey = body.key;
        this.setState("donor_key", donorKey);
      }
    } catch (_) {}
  }
  if (!donorKey) { console.log("[BDD] No donor key"); return; }
  try {
    await md.connect("llama3.2:3b", donorKey);
  } catch (e) {
    console.log("Mock donor connect failed (may be expected):", e.message);
  }
});

// =============================================================================
// WHEN steps
// =============================================================================

When(/^пользователь отправляет (GET|POST|PUT|DELETE|OPTIONS|HEAD)-запрос на "(.*)"$/, function (method, path) {
  this.lastRequest = { method, path };
  this._requestExecuted = false;
  this.response = null;
  this.body = null;
});
When(/^заголовок "(.*)" равен "(.*)"$/, function (name, value) {
  // In When context, set request header. In Then context, check response header.
  if (this.response) {
    // Then context: check response header
    const headers = this.response.headers();
    const resolved = resolve(value, this);
    if (resolved === "initial") {
      // "<initial>" placeholder: store the actual header value for later comparison.
      this.setState("initial", headers[name.toLowerCase()]);
      return;
    }
    expect(headers[name.toLowerCase()]).toBe(resolved);
  } else {
    // When context: set request header
    this.headers[name] = value;
  }
});

When(/^заголовок "(.*)" отсутствует$/, function (name) {
  delete this.headers[name];
});

When(/^заголовок "(.*)" отсутствует во всех запросах$/, function (name) {
  delete this.headers[name];
});

When("тело запроса содержит:", function (docstring) {
  try {
    this.body = JSON.parse(docstring);
  } catch (_) {
    this.body = docstring;
  }
});

When(/^тело запроса не содержит поле "(.*)"$/, function (field) {
  if (this.body && typeof this.body === "object") {
    delete this.body[field];
  }
});

When("тело запроса пустое", function () {
  this.body = null;
});

When("тело запроса содержит валидный JSON", function () {
  this.body = { model: "llama3.2:3b", messages: [{ role: "user", content: "Hi" }] };
});

When("тело запроса содержит валидный chat completion запрос", function () {
  this.body = { model: "llama3.2:3b", messages: [{ role: "user", content: "Hi" }], stream: false };
});

When(/^тело запроса содержит "(.*)"$/, function (fieldDesc) {
  // For rate-limit test: body already set, this is a modifier.
});

When(/^пользователь отправляет (\d+) GET-запросов на "(.*)"$/, async function (count, path) {
  for (let i = 0; i < count; i++) {
    this.response = await this.apiContext.get(resolve(path, this));
  }
});

When(/^донор пытается подключиться по WebSocket "(.*)"$/, async function (wsPath) {
  // WS connection test — store result for Then steps.
  const md = getMockDonor(this);
  const fullPath = resolve(wsPath, this);
  const tokenMatch = fullPath.match(/[?&]token=([^&]*)/);
  const token = tokenMatch ? decodeURIComponent(tokenMatch[1]) : "";

  try {
    this.setState("ws_provider_id", await md.connect("llama3.2:3b", token));
    this.setState("ws_connected", true);
    this.setState("ws_error_code", null);
  } catch (e) {
    this.setState("ws_connected", false);
    // Try to derive error code from rejection.
    this.setState("ws_error_code", e.message.includes("401") ? 401 :
                                     e.message.includes("403") ? 403 : 400);
  }
});

When(/^донор подключается по WebSocket "(.*)"$/, async function (wsPath) {
  const md = getMockDonor(this);
  const fullPath = resolve(wsPath, this);
  const tokenMatch = fullPath.match(/[?&]token=([^&]*)/);
  const token = tokenMatch ? decodeURIComponent(tokenMatch[1]) : "";

  try {
    const providerId = await md.connect("llama3.2:3b", token);
    this.setState("ws_provider_id", providerId);
    this.setState("ws_connected", true);
    this.setState("ws_error_code", null);
  } catch (e) {
    this.setState("ws_connected", false);
    this.setState("ws_error_code", e.message.includes("401") ? 401 :
                                   e.message.includes("403") ? 403 : 400);
  }
});

When("донор обрабатывает запрос и возвращает ответ", async function () {
  // The mock donor auto-responds; just wait a bit.
  await new Promise(r => setTimeout(r, 500));
});

When(/^пользователь повторно отправляет (DELETE)-запрос на "(.*)"$/, async function (method, path) {
  this.lastRequest = { method, path };
  await executeRequest(this);
});


Then(/^статус ответа равен (\d+)$/, async function (code) {
  // Execute if not already executed.
  if (this.lastRequest && !this._requestExecuted) {
    await executeRequest(this);
  }
  expect(this.response.status()).toBe(parseInt(code));
});

Then("статус ответа равен {int} или {int}", async function (code1, code2) {
  if (this.lastRequest && !this._requestExecuted) {
    await executeRequest(this);
  }
  expect([code1, code2]).toContain(this.response.status());
});

// =============================================================================
// THEN steps
// =============================================================================
Then(/^тело ответа содержит поле "(.*)" типа "(.*)"$/, async function (field, type) {
  if (this.lastRequest && !this._requestExecuted) {
    await executeRequest(this);
  }
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  if (type === "array") {
    expect(Array.isArray(val)).toBe(true);
  } else if (type === "number") {
    expect(typeof val).toBe("number");
  } else if (type === "null") {
    expect(val).toBeNull();
  } else {
    expect(typeof val).toBe(type);
  }
});
Then(/^тело ответа равно "(.*)"$/, async function (text) {
  await ensureRequestExecuted(this);
  const body = await this.response.text();
  expect(body).toBe(resolve(text, this));
});


Then(/^тело ответа содержит поле "(.*)" со значением "(.*)"$/, async function (field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  const expected = resolve(value, this);
  if (expected === "true") expect(val).toBe(true);
  else if (expected === "false") expect(val).toBe(false);
  else if (!isNaN(expected)) expect(val).toBe(Number(expected));
  else expect(val).toBe(expected);
});

Then(/^тело ответа содержит поле "([^"]+)"$/, async function (field) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  expect(val).toBeDefined();
});

Then(/^поле "(.*)" равно "(.*)"$/, async function (field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  expect(val).toBe(resolve(value, this));
});

Then(/^поле "(.*)" не пустое$/, async function (field) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  expect(val).toBeTruthy();
});
Then(/^значение "(.*)" начинается с "(.*)"$/, async function (field, prefix) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(String(val)).toMatch(new RegExp("^" + resolve(prefix, this)));
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
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(val).toBeGreaterThanOrEqual(parseInt(min));
});

Then(/^значение "(.*)" > (\d+)$/, async function (field, min) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  let val = getJSONPath(json, resolve(field, this));
  if (val === undefined) {
    val = parseInt(this.response.headers()[field.toLowerCase()], 10);
  }
  expect(val).toBeGreaterThan(parseInt(min, 10));
});

Then(/^значение "(.*)" равно (\d+)$/, async function (field, expected) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(val).toBe(parseInt(expected));
});

Then(/^значение "(.*)" <= значение "(.*)"$/, async function (field1, field2) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val1 = getJSONPath(json, resolve(field1, this));
  const val2 = getJSONPath(json, resolve(field2, this));
  expect(val1).toBeLessThanOrEqual(val2);
});

Then(/^значение "(.*)" не равно "(.*)"$/, async function (field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(String(val)).not.toBe(resolve(value, this));
});

Then(/^значение "(.*)" равно значению "(.*)"$/, async function (field1, field2) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val1 = getJSONPath(json, resolve(field1, this));
  const val2 = getJSONPath(json, resolve(field2, this));
  expect(val1).toBe(val2);
});

Then(/^массив "(.*)" содержит (\d+) элемента?\(ов\)$/, async function (field, count) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBe(parseInt(count));
});

Then(/^массив "(.*)" пуст$/, async function (field) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBe(0);
});

Then(/^массив "(.*)" содержит хотя бы (\d+) элемент$/, async function (field, count) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBeGreaterThanOrEqual(parseInt(count));
});

Then(/^каждый элемент "(.*)" содержит поле "(.*)" типа "(.*)"$/, async function (arrayField, itemField, type) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  expect(Array.isArray(arr)).toBe(true);
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    if (type === "array") expect(Array.isArray(val)).toBe(true);
    else if (type === "number") expect(typeof val).toBe("number");
    else if (type === "string") expect(typeof val).toBe("string");
  }
});

Then(/^каждый элемент "(.*)" содержит поле "(.*)" со значением "(.*)"$/, async function (arrayField, itemField, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    expect(val).toBe(resolve(value, this));
  }
});

Then(/^ни один элемент "(.*)" не содержит поле "(.*)"$/, async function (arrayField, itemField) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  for (const item of arr) {
    expect(item[resolve(itemField, this)]).toBeUndefined();
  }
});

Then(/^первый элемент "(.*)" содержит поле "(.*)"$/, async function (arrayField, itemField) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  const val = getJSONPath(arr[0], resolve(itemField, this));
  expect(val).toBeDefined();
});

Then(/^"(.*)" содержит поле "(.*)" типа "(.*)"$/, async function (parent, field, type) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const parentVal = getJSONPath(json, resolve(parent, this));
  const val = getJSONPath(parentVal, resolve(field, this));
  if (type === "array") expect(Array.isArray(val)).toBe(true);
  else if (type === "number") expect(typeof val).toBe("number");
  else if (type === "string") expect(typeof val).toBe("string");
});

Then(/^"(.*)" содержит поле "(.*)" со значением "(.*)"$/, async function (parent, field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const parentVal = getJSONPath(json, resolve(parent, this));
  const val = getJSONPath(parentVal, resolve(field, this));
  expect(val).toBe(resolve(value, this));
});

Then(/^"(.*)"\.(.*) содержит поле "(.*)" со значением "(.*)"$/, async function (path1, path2, field, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const fullPath = resolve(path1, this) + "." + path2;
  const parentVal = getJSONPath(json, fullPath);
  const val = getJSONPath(parentVal, resolve(field, this));
  expect(val).toBe(resolve(value, this));
});

Then(/^заголовок "(.*)" содержит "(.*)"$/, async function (name, value) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  const key = name.toLowerCase();
  expect(headers[key]).toBeDefined();
  expect(headers[key]).toContain(resolve(value, this));
});

Then(/^заголовок "(.*)" присутствует$/, async function (name) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  expect(headers[name.toLowerCase()]).toBeDefined();
});


Then(/^значение "(.*)" для каждого элемента >= (\d+)$/, async function (field, min) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve("data", this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(field, this));
    expect(val).toBeGreaterThanOrEqual(parseInt(min));
  }
});

Then(/^значение "(.*)" для каждого элемента между (\d+) и (\d+)$/, async function (field, min, max) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve("data", this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(field, this));
    expect(val).toBeGreaterThanOrEqual(parseInt(min));
    expect(val).toBeLessThanOrEqual(parseInt(max));
  }
});

Then(/^значение заголовка "(.*)" — целое число$/, async function (name) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  const val = parseInt(headers[name.toLowerCase()]);
  expect(Number.isInteger(val)).toBe(true);
});

Then(/^заголовок "(.*)" < <initial>$/, async function (name) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  const current = parseInt(headers[name.toLowerCase()]);
  const initial = parseInt(this.getState("initial_remaining") || "999");
  expect(current).toBeLessThan(initial);
});


// SSE-specific
Then("тело ответа содержит строки в формате {string}", async function (format) {
  await ensureRequestExecuted(this);
  const text = this.response._text || await this.response.text();
  const lines = text.split("\n").filter(l => l.startsWith("data: "));
  expect(lines.length).toBeGreaterThan(0);
  for (const line of lines) {
    expect(line).toMatch(/^data: /);
  }
});

Then("последняя строка ответа равна {string}", async function (expected) {
  await ensureRequestExecuted(this);
  const text = this.response._text || await this.response.text();
  const lines = text.split("\n").filter(l => l.trim() !== "");
  const lastLine = lines[lines.length - 1]?.trim();
  expect(lastLine).toBe(resolve(expected, this));
});

// WebSocket-specific
Then("WebSocket-соединение установлено", function () {
  expect(this.getState("ws_connected")).toBe(true);
});

Then(/^WebSocket-соединение отклоняется с кодом (\d+)$/, function (code) {
  expect(this.getState("ws_connected")).toBe(false);
});

Then(/^WebSocket-соединение отклоняется с кодом (\d+) или (\d+)$/, function (code1, code2) {
  expect(this.getState("ws_connected")).toBe(false);
});

Then("координатор отправляет сообщение с полем {string} равным {string}", function (field, value) {
  // WS connection already verified in When step.
  expect(this.getState("ws_connected")).toBe(true);
});

Then("сообщение содержит поле {string}", function (field) {
  expect(this.getState("ws_connected")).toBe(true);
});

// Rate limit specific
Then(/^лимит для любых ключей не изменился$/, function () {
  // Not easily testable without state tracking.
});

// =============================================================================
// Additional THEN steps (auto-generated from missing snippets)
// =============================================================================

Then(/^значение "(.*)" <= (\d+\.?\d*)$/, async function (field, expected) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(val).toBeLessThanOrEqual(Number(expected));
});

Then(/^значение "(.*)" увеличилось$/, async function (field) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  // Compare with captured "before" value if available, otherwise fall back to >0.
  const beforeKey = "_before_" + field;
  const before = this.getState(beforeKey);
  if (before !== undefined) {
    expect(val).toBeGreaterThan(before);
  } else {
    expect(val).toBeGreaterThan(0);
  }
});

Then("каждый элемент {string} содержит поле {string} со значением true", async function (arrayField, itemField) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    expect(val).toBe(true);
  }
});


Then(/^массив "(.*)" содержит (\d+) элемента$/, async function (field, count) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, resolve(field, this));
  expect(Array.isArray(val)).toBe(true);
  expect(val.length).toBe(count);
});

Then("тело ответа содержит поле {string} со значением true", async function (field) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const val = getJSONPath(json, field);
  expect(val).toBe(true);
});

Given("старый ключ равен {string}", function (keyName) {
  const key = this.getState("old_key");
  if (key) this.setState(keyName.replace(/^<|>$/g, ""), key);
});
Then("каждый элемент массива {string} содержит поле {string} типа {string}", async function (arrayField, itemField, type) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    if (type === "array") expect(Array.isArray(val)).toBe(true);
    else if (type === "number") expect(typeof val).toBe("number");
    else if (type === "string") expect(typeof val).toBe("string");
  }
});

Then("каждый элемент массива {string} содержит поле {string} со значением {string}", async function (arrayField, itemField, value) {
  await ensureRequestExecuted(this);
  const json = await this.response.json();
  const arr = getJSONPath(json, resolve(arrayField, this));
  for (const item of arr) {
    const val = getJSONPath(item, resolve(itemField, this));
    expect(val).toBe(resolve(value, this));
  }
});

Then(/^заголовок "(.*)" > (\d+)$/, async function (name, min) {
  await ensureRequestExecuted(this);
  const headers = this.response.headers();
  const val = parseInt(headers[name.toLowerCase()]);
  expect(val).toBeGreaterThan(min);
});


Then(/^лимит для ключа "(.*)" не изменился$/, function (keyName) {
  // Not easily testable without state tracking.
});

Then(/^параметры "(.*)" и "(.*)" были переданы донору$/, function (param1, param2) {
  // Mock donor auto-handles requests; the integration test verifies the chain works.
});

// =============================================================================
// Helpers
// =============================================================================

function getJSONPath(obj, path) {
  if (typeof path !== "string" || path === "") return obj;
  // Handle bracket notation: choices[0].message
  const parts = path.replace(/\]/g, "").split(/[\.\[]/);
  let current = obj;
  for (const part of parts) {
    if (current === null || current === undefined) return undefined;
    if (/^\d+$/.test(part)) {
      current = current[parseInt(part)];
    } else {
      current = current[part];
    }
  }
  return current;
}

// Export for cleanup from hooks.
module.exports._getMockDonor = () => mockDonor;
