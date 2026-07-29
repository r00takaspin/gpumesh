const WebSocket = require("ws");

/**
 * Mock donor for BDD tests.
 *
 * Connects to the coordinator WebSocket, registers with models,
 * and can respond to inference requests with canned responses.
 *
 * Protocol per SPEC.md §3.4.
 */

class MockDonor {
  constructor(baseUrl) {
    // Convert http:// to ws://
    this.wsUrl = baseUrl.replace(/^http/, "ws") + "/ws/provider";
    this.baseUrl = baseUrl;
    this.nextId = 1;
    // Map<providerId, { ws, models, load, pendingRequests }>
    this.connections = new Map();
    // Callbacks for request inspection (used by parameter-passing tests).
    this.requestHandlers = new Map();
  }

  /**
   * Connect to the coordinator and register with a model.
   * Returns the assigned providerId.
   */
  async connect(model, token) {
    const wsUrl = token ? `${this.wsUrl}?token=${encodeURIComponent(token)}` : this.wsUrl;
    console.log(`[MockDonor] Connecting to ${wsUrl} with model=${model}`);
    const ws = new WebSocket(wsUrl);

    // Set up ALL handlers BEFORE waiting for open.
    let providerId = null;
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
      console.log(`[MockDonor] WS open, sending register for ${model}`);
      ws.send(JSON.stringify({
        type: "register",
        models: [model],
        max_concurrent: 5,
        description: "test donor",
        hardware: "test"
      }));
    });

    ws.on("message", (data) => {
      try {
        const msg = JSON.parse(data.toString());
        console.log(`[MockDonor] Received message type=${msg.type}`);
        if (msg.type === "registered") {
          providerId = msg.provider_id;
          this.connections.set(providerId, conn);
          console.log(`[MockDonor] Registered as provider_id=${providerId}`);
          registrationDone = true;
        } else if (msg.type === "request" && providerId) {
          this._handleRequest(providerId, msg);
        } else if (msg.type === "cancel" && providerId) {
          conn.pendingRequests.delete(msg.request_id);
        }
        // heartbeat_ack — ignore
      } catch (_) {}
    });

    ws.on("error", (err) => {
      console.log(`[MockDonor] WS error: ${err.message}`);
      wsError = err;
      registrationDone = true; // unblock wait
    });

    ws.on("close", (code) => {
      console.log(`[MockDonor] WS closed for provider_id=${providerId}, code=${code}`);
      if (providerId) this.connections.delete(providerId);
      closeCode = code;
      registrationDone = true; // unblock wait
    });

    // Start heartbeat.
    const heartbeat = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "heartbeat" }));
      }
    }, 30000);
    ws.on("close", () => clearInterval(heartbeat));

    // Wait for registration (or error).
    await new Promise((resolve) => {
      const check = setInterval(() => {
        if (registrationDone) {
          clearInterval(check);
          resolve();
        }
      }, 50);
      // Safety timeout
      setTimeout(() => {
        clearInterval(check);
        resolve();
      }, 15000);
    });

    // Throw on auth failure so tests can detect rejection.
    if (!providerId) {
      if (wsError && wsError.message) {
        // Extract HTTP status from WebSocket error message like "Unexpected server response: 401"
        const codeMatch = wsError.message.match(/(\d{3})/);
        throw new Error(`WS connect failed: ${codeMatch ? codeMatch[1] : "400"} ${wsError.message}`);
      }
      throw new Error(`WS connect failed: close_code=${closeCode || "unknown"}`);
    }

    return providerId;
  }

  /**
   * Disconnect a donor by providerId.
   */
  disconnect(providerId) {
    const conn = this.connections.get(providerId);
    if (conn) {
      conn.ws.close();
      this.connections.delete(providerId);
    }
  }

  /**
   * Disconnect all mock donors.
   */
  disconnectAll() {
    for (const [providerId, conn] of this.connections) {
      conn.ws.close();
    }
    this.connections.clear();
  }

  /**
   * Set the load on a donor (for "all donors busy" tests).
   */
  setLoad(providerId, load) {
    const conn = this.connections.get(providerId);
    if (conn) {
      conn.load = load;
      // Send a message to update load at coordinator if needed.
      // For now, the test scenario controls this via mock.
    }
  }

  /**
   * Set a custom handler for incoming requests on a provider.
   * handler(providerId, requestMsg) should call sendResponse/sendChunks.
   */
  onRequest(providerId, handler) {
    this.requestHandlers.set(providerId, handler);
  }

  /**
   * Send a non-streaming response from a donor.
   */
  sendResponse(providerId, requestId, content, model, usage) {
    const conn = this.connections.get(providerId);
    if (!conn || conn.ws.readyState !== WebSocket.OPEN) return;

    conn.ws.send(JSON.stringify({
      type: "response",
      request_id: requestId,
      content: content || "Hello from test donor",
      model: model || "llama3.2:3b",
      usage: usage || { prompt_tokens: 10, completion_tokens: 20, total_tokens: 30 }
    }));
  }

  /**
   * Send a streaming response (multiple chunks, ending with done).
   */
  sendChunks(providerId, requestId, chunks, model) {
    const conn = this.connections.get(providerId);
    if (!conn || conn.ws.readyState !== WebSocket.OPEN) return;

    for (const chunk of chunks) {
      conn.ws.send(JSON.stringify({
        type: "chunk",
        request_id: requestId,
        content: chunk,
        done: false
      }));
    }

    // Final done chunk.
    conn.ws.send(JSON.stringify({
      type: "chunk",
      request_id: requestId,
      content: "",
      done: true
    }));
  }

  /**
   * Send an error from a donor.
   */
  sendError(providerId, requestId, code, message) {
    const conn = this.connections.get(providerId);
    if (!conn || conn.ws.readyState !== WebSocket.OPEN) return;

    conn.ws.send(JSON.stringify({
      type: "error",
      request_id: requestId,
      code: code || "internal",
      message: message || "test error"
    }));
  }

  /**
   * Get the models registered by a donor.
   */
  getModels(providerId) {
    const conn = this.connections.get(providerId);
    return conn ? conn.models : [];
  }

  /**
   * Check if a donor is connected.
   */
  isConnected(providerId) {
    const conn = this.connections.get(providerId);
    return conn ? conn.ws.readyState === WebSocket.OPEN : false;
  }

  // --- Internal ---

  /**
   * Check if any donor is connected.
   */
  isAnyConnected() {
    for (const conn of this.connections.values()) {
      if (conn.ws.readyState === WebSocket.OPEN) {
        return true;
      }
    }
    return false;
  }


  _handleRequest(providerId, msg) {
    const handler = this.requestHandlers.get(providerId);
    if (handler) {
      handler(providerId, msg);
      return;
    }

    // Default: auto-respond with a canned reply.
    if (msg.stream) {
      this.sendChunks(providerId, msg.request_id, ["Hello", " from", " test", " donor"], msg.model);
    } else {
      this.sendResponse(providerId, msg.request_id, "Hello from test donor", msg.model);
    }
  }
}

module.exports = { MockDonor };

