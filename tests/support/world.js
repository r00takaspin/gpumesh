const { setWorldConstructor } = require("@cucumber/cucumber");

class GpumeshWorld {
  constructor({ parameters }) {
    this.baseUrl = process.env.COORDINATOR_URL || "http://localhost:8080";
    this.browser = null;
    this.page = null;
    this.apiContext = null;
    this.response = null;
    this.headers = {};
    this.body = null;
    this.testState = new Map();
    this.lastRequest = null;
    this._requestContexts = new Set();
  }

  async initBrowser() {
    if (!this.browser) {
      const { chromium } = require("playwright");
      this.browser = await chromium.launch({ headless: true });
    }
    this.page = await this.browser.newPage();
  }

  async initApiContext() {
    const { request } = require("playwright");
    this.apiContext = await request.newContext({
      baseURL: this.baseUrl
    });
    this.trackRequestContext(this.apiContext);
  }

  trackRequestContext(ctx) {
    if (ctx) this._requestContexts.add(ctx);
    return ctx;
  }

  async newApiContext(extra = {}) {
    const { request } = require("playwright");
    const ctx = await request.newContext({
      baseURL: this.baseUrl,
      ...extra,
    });
    return this.trackRequestContext(ctx);
  }

  async disposeRequestContexts() {
    for (const ctx of this._requestContexts) {
      try { await ctx.dispose(); } catch (_) {}
    }
    this._requestContexts.clear();
  }

  setState(key, value) { this.testState.set(key, value); }
  getState(key) { return this.testState.get(key); }
}

setWorldConstructor(GpumeshWorld);
