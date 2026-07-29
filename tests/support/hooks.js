const { Before, After, BeforeAll, AfterAll } = require("@cucumber/cucumber");
const { execSync, spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

let coordinatorProcess = null;
const COORDINATOR_URL = process.env.COORDINATOR_URL || "http://localhost:8080";
const TEST_DB = path.resolve(__dirname, "../../data/test-gpumesh.db");

// --- BeforeAll / AfterAll ---

BeforeAll({ timeout: 30000 }, async function () {
  // If COORDINATOR_URL points to an external coordinator, skip startup.
  if (process.env.COORDINATOR_URL) {
    // Verify it's reachable.
    await waitForHealth(COORDINATOR_URL);
    return;
  }

  // Clean up any stale test DB.
  try { fs.unlinkSync(TEST_DB); } catch (_) {}

  // Start coordinator in test mode.
  const repoRoot = path.resolve(__dirname, "../..");
  coordinatorProcess = spawn("go", ["run", "./cmd/coordinator"], {
    cwd: repoRoot,
    env: {
      ...process.env,
      TEST_MODE: "true",
      MESH_ADDR: ":8080",
      MESH_DB: TEST_DB,
      MESH_BASE_URL: "http://localhost:8080",
      MESH_RATE_LIMIT: "100",
      GITHUB_CLIENT_ID: "test-client-id",
      GITHUB_CLIENT_SECRET: "test-client-secret",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let startupOutput = "";
  coordinatorProcess.stdout.on("data", (d) => { startupOutput += d.toString(); });
  coordinatorProcess.stderr.on("data", (d) => { startupOutput += d.toString(); });

  // Wait for the coordinator to be ready.
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      reject(new Error("Coordinator did not start within 30s.\n" + startupOutput));
    }, 30000);
    const check = setInterval(async () => {
      try {
        const res = await fetch(COORDINATOR_URL + "/health");
        if (res.status === 200) {
          clearTimeout(timeout);
          clearInterval(check);
          resolve();
        }
      } catch (_) {}
    }, 500);
  });
});

AfterAll({ timeout: 10000 }, async function () {
  // Close all mock-donor WebSocket connections (keeps event loop alive).
  try {
    const apiSteps = require("../steps/api.steps");
    if (apiSteps._getMockDonor) {
      const md = apiSteps._getMockDonor();
      if (md) md.disconnectAll();
    }
  } catch (_) {}

  if (coordinatorProcess) {
    coordinatorProcess.kill("SIGTERM");
    coordinatorProcess = null;
  }
  try { fs.unlinkSync(TEST_DB); } catch (_) {}

  // Give the coordinator a moment to shut down.
  await new Promise(r => setTimeout(r, 500));
});

// --- Before / After hooks ---

Before({ tags: "@api" }, async function () {
  await this.initApiContext();
  this.testState.clear();
  this.headers = {};
  this.body = null;
  this.response = null;
  this.lastRequest = null;
  this._requestExecuted = false;
});

Before({ tags: "@ui" }, async function () {
  await this.initBrowser();
});

After(async function () {
  // Clean up mock-donor connections after EVERY scenario.
  try {
    const { _getMockDonor } = require("../steps/api.steps");
    const md = _getMockDonor();
    if (md) {
      md.disconnectAll();
    }
  } catch (_) {}
  // Close API context.
  if (this.apiContext) {
    await this.apiContext.dispose();
    this.apiContext = null;
  }
});

After({ tags: "@ui" }, async function () {
  if (this.page) {
    await this.page.close();
    this.page = null;
  }
  if (this.browser) {
    await this.browser.close();
    this.browser = null;
  }
});

// --- Helpers ---

async function waitForHealth(url, maxRetries = 60) {
  for (let i = 0; i < maxRetries; i++) {
    try {
      const res = await fetch(url + "/health");
      if (res.status === 200) return;
    } catch (_) {}
    await new Promise(r => setTimeout(r, 500));
  }
  throw new Error("Coordinator not reachable at " + url);
}
