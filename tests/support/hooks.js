const { Before, After, BeforeAll, AfterAll } = require("@cucumber/cucumber");
const { spawn, execFileSync } = require("child_process");
const path = require("path");
const fs = require("fs");
const { disconnectAllMockDonors } = require("./mock-donor");

let coordinatorProcess = null;
const COORDINATOR_URL = process.env.COORDINATOR_URL || "http://localhost:8080";
const TEST_DB = path.resolve(__dirname, "../../data/test-gpumesh.db");
const COORD_BIN = path.resolve(__dirname, "../../.tmp/gpumesh-coordinator-test");

// --- BeforeAll / AfterAll ---

BeforeAll({ timeout: 60000 }, async function () {
  // If COORDINATOR_URL points to an external coordinator, skip startup.
  if (process.env.COORDINATOR_URL) {
    await waitForHealth(COORDINATOR_URL);
    return;
  }

  try { fs.unlinkSync(TEST_DB); } catch (_) {}
  fs.mkdirSync(path.dirname(COORD_BIN), { recursive: true });

  const repoRoot = path.resolve(__dirname, "../..");
  // Build a binary once — `go run` leaves child processes / pipe handles that hang Node after tests.
  execFileSync("go", ["build", "-o", COORD_BIN, "./cmd/coordinator"], {
    cwd: repoRoot,
    stdio: "pipe",
    env: process.env,
  });

  coordinatorProcess = spawn(COORD_BIN, [], {
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
    // Detached process group so AfterAll can SIGKILL the whole tree; ignore stdio so pipes
    // don't keep the cucumber event loop alive after the suite finishes.
    stdio: "ignore",
    detached: true,
  });
  // Don't keep Node alive waiting on the coordinator child.
  if (typeof coordinatorProcess.unref === "function") coordinatorProcess.unref();

  await waitForHealth(COORDINATOR_URL);
});

AfterAll({ timeout: 15000 }, async function () {
  disconnectAllMockDonors();

  try {
    const apiSteps = require("../steps/api.steps");
    if (apiSteps._resetMockDonor) apiSteps._resetMockDonor();
  } catch (_) {}

  if (coordinatorProcess && coordinatorProcess.pid) {
    await killCoordinator(coordinatorProcess.pid);
    coordinatorProcess = null;
  }
  try { fs.unlinkSync(TEST_DB); } catch (_) {}
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
  // UI steps keep MockDonor on world state; API steps use a module singleton — close both.
  const worldDonor = this.getState && this.getState("mock_donor");
  if (worldDonor && typeof worldDonor.disconnectAll === "function") {
    try { worldDonor.disconnectAll(); } catch (_) {}
  }
  if (this.setState) this.setState("mock_donor", null);
  disconnectAllMockDonors();

  try {
    const apiSteps = require("../steps/api.steps");
    if (apiSteps._resetMockDonor) apiSteps._resetMockDonor();
  } catch (_) {}

  if (this.disposeRequestContexts) {
    await this.disposeRequestContexts();
  }
  if (this.apiContext) {
    try { await this.apiContext.dispose(); } catch (_) {}
    this.apiContext = null;
  }
});

After({ tags: "@ui" }, async function () {
  if (this.page) {
    try { await this.page.close(); } catch (_) {}
    this.page = null;
  }
  if (this.browser) {
    try { await this.browser.close(); } catch (_) {}
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

function killCoordinator(pid) {
  return new Promise((resolve) => {
    const tryKill = (signal) => {
      try { process.kill(-pid, signal); } catch (_) {
        try { process.kill(pid, signal); } catch (_) {}
      }
    };
    tryKill("SIGTERM");
    const deadline = Date.now() + 2000;
    const check = setInterval(() => {
      let alive = false;
      try {
        process.kill(pid, 0);
        alive = true;
      } catch (_) {}
      if (!alive || Date.now() > deadline) {
        clearInterval(check);
        if (alive) tryKill("SIGKILL");
        // Brief settle so port/handles release before Node exits.
        setTimeout(resolve, 50);
      }
    }, 50);
  });
}
