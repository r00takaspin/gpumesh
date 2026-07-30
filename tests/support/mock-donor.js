const WebSocket = require("ws");

/**
 * Mock provider agent for BDD tests (SPEC-v2).
 * Connects to /ws/provider, registers, auto-answers inference requests.
 * Connection key is stable machine_id from registered message.
 */
class MockDonor {
  constructor(baseUrl) {
    this.wsUrl = baseUrl.replace(/^http/, "ws") + "/ws/provider";
    this.baseUrl = baseUrl;
    this.connections = new Map(); // machine_id → conn
    this.requestHandlers = new Map();
  }

  async connect(model, token) {
    const wsUrl = token ? `${this.wsUrl}?token=${encodeURIComponent(token)}` : this.wsUrl;
    const ws = new WebSocket(wsUrl);

    let machineId = null;
    let sessionId = null;
    let registrationDone = false;
    let wsError = null;
    let closeCode = null;

    const conn = {
      ws,
      models: [model],
      load: 0,
      pendingRequests: new Map(),
    };

    ws.on("open", () => {
      ws.send(JSON.stringify({
        type: "register",
        models: [model],
        max_concurrent: 5,
        description: "test-provider",
        hardware: "test",
      }));
    });

    ws.on("message", (data) => {
      try {
        const msg = JSON.parse(data.toString());
        if (msg.type === "registered") {
          machineId = msg.machine_id || msg.provider_id;
          sessionId = msg.provider_id || "";
          conn.machineId = machineId;
          conn.sessionId = sessionId;
          this.connections.set(machineId, conn);
          registrationDone = true;
        } else if (msg.type === "request" && machineId) {
          this._handleRequest(machineId, msg);
        } else if (msg.type === "cancel" && machineId) {
          conn.pendingRequests.delete(msg.request_id);
        }
      } catch (_) {}
    });

    ws.on("error", (err) => {
      wsError = err;
      registrationDone = true;
    });

    ws.on("close", (code) => {
      if (machineId) this.connections.delete(machineId);
      closeCode = code;
      registrationDone = true;
    });

    const heartbeat = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "heartbeat" }));
      }
    }, 30000);
    ws.on("close", () => clearInterval(heartbeat));

    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (registrationDone) {
          clearInterval(check);
          resolve();
        }
      }, 50);
      setTimeout(() => {
        clearInterval(check);
        resolve();
      }, 15000);
    });

    if (!machineId) {
      if (wsError && wsError.message) {
        const codeMatch = wsError.message.match(/(\d{3})/);
        throw new Error(`WS connect failed: ${codeMatch ? codeMatch[1] : "400"} ${wsError.message}`);
      }
      throw new Error(`WS connect failed: close_code=${closeCode || "unknown"}`);
    }

    return machineId;
  }

  disconnect(machineId) {
    const conn = this.connections.get(machineId);
    if (conn) {
      conn.ws.close();
      this.connections.delete(machineId);
    }
  }

  disconnectAll() {
    for (const [, conn] of this.connections) {
      try { conn.ws.close(); } catch (_) {}
    }
    this.connections.clear();
  }

  sendResponse(machineId, requestId, content, model, usage) {
    const conn = this.connections.get(machineId);
    if (!conn || conn.ws.readyState !== WebSocket.OPEN) return;
    conn.ws.send(JSON.stringify({
      type: "response",
      request_id: requestId,
      content: content || "Hello from test provider",
      model: model || "llama3.2:3b",
      usage: usage || { prompt_tokens: 10, completion_tokens: 20, total_tokens: 30 },
    }));
  }

  sendChunks(machineId, requestId, chunks) {
    const conn = this.connections.get(machineId);
    if (!conn || conn.ws.readyState !== WebSocket.OPEN) return;
    for (const chunk of chunks) {
      conn.ws.send(JSON.stringify({
        type: "chunk",
        request_id: requestId,
        content: chunk,
        done: false,
      }));
    }
    conn.ws.send(JSON.stringify({
      type: "chunk",
      request_id: requestId,
      content: "",
      done: true,
    }));
  }

  isAnyConnected() {
    for (const conn of this.connections.values()) {
      if (conn.ws.readyState === WebSocket.OPEN) return true;
    }
    return false;
  }

  _handleRequest(machineId, msg) {
    const handler = this.requestHandlers.get(machineId);
    if (handler) {
      handler(machineId, msg);
      return;
    }
    if (msg.stream) {
      this.sendChunks(machineId, msg.request_id, ["Hello", " from", " test", " provider"]);
    } else {
      this.sendResponse(machineId, msg.request_id, "Hello from test provider", msg.model);
    }
  }
}

module.exports = { MockDonor };
