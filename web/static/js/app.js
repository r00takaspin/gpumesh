// GPU Mesh v2 — JS helpers (vanilla, no framework)

function copyToClipboard(btn) {
  let text = btn.getAttribute('data-copy');
  let codeBlock = null;
  if (!text) {
    const card = btn.closest('.card, .consumer-card, .modal');
    if (card) {
      const pre = card.querySelector('pre, .code, .pin, .key');
      if (pre) {
        text = pre.textContent;
        codeBlock = pre;
      }
    }
  }
  if (!text) return;
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text.trim()).then(function () {
      flashCopied(btn, codeBlock);
    }).catch(function () {
      fallbackCopy(text, btn, codeBlock);
    });
  } else {
    fallbackCopy(text, btn, codeBlock);
  }
}

function fallbackCopy(text, btn, codeBlock) {
  const ta = document.createElement('textarea');
  ta.value = text.trim();
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  document.body.removeChild(ta);
  flashCopied(btn, codeBlock);
}

function flashCopied(btn, codeBlock) {
  const orig = btn.textContent;
  btn.textContent = 'Copied!';
  btn.classList.add('copied');
  if (codeBlock) codeBlock.classList.add('flash-copied');
  setTimeout(function () {
    btn.textContent = orig;
    btn.classList.remove('copied');
    if (codeBlock) codeBlock.classList.remove('flash-copied');
  }, 2000);
}

function doCopy(btn, text) {
  var ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); } catch (e) { /* ignore */ }
  document.body.removeChild(ta);
  btn.textContent = 'Copied!';
  btn.classList.add('copied');
  setTimeout(function () {
    btn.textContent = btn.getAttribute('data-label') || 'Copy';
    btn.classList.remove('copied');
  }, 2000);
}

function copyCode(btn) {
  var pre = btn.closest('pre') || btn.parentElement.querySelector('.code, pre');
  if (!pre) return;
  var code = pre.querySelector('code');
  var text = code ? code.textContent : pre.textContent;
  doCopy(btn, text.trim());
}

function copyText(btn, text) {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text).then(function () {
      flashCopied(btn, null);
    }).catch(function () {
      doCopy(btn, text);
    });
  } else {
    doCopy(btn, text);
  }
}

function copyKey(btn, key) {
  copyText(btn, key);
}

function dismissKey() {
  var el = document.getElementById('onetime-key');
  if (el) el.style.display = 'none';
  var w = document.getElementById('onetime-warning');
  if (w) w.style.display = 'none';
}

function resolveApiKey() {
  var banner = document.getElementById('onetime-key');
  if (banner && banner.style.display !== 'none') {
    var el = banner.querySelector('[data-testid=onetime-key-value]');
    if (el) {
      var key = (el.textContent || '').trim();
      if (key) return key;
    }
  }
  var modal = document.getElementById('new-key-modal');
  if (modal) {
    var el2 = modal.querySelector('[data-testid=new-key-value]');
    if (el2) {
      var k = (el2.textContent || '').trim();
      if (k) return k;
    }
  }
  return 'YOUR_API_KEY';
}

function fillSnippet(panel, kind) {
  if (!panel) return;
  var code = panel.querySelector('[data-testid=snippet-code]');
  if (!code) return;
  code.textContent = buildSnippet(
    kind || 'curl',
    code.getAttribute('data-base-url'),
    code.getAttribute('data-model') || 'llama3.2:3b'
  );
}

function toggleSetup(btn) {
  var card = btn.closest('[data-testid=machine-card]');
  if (!card) return;
  var panel = card.querySelector('[data-testid=setup-panel]');
  if (!panel) return;
  panel.classList.toggle('hidden');
  if (!panel.classList.contains('hidden')) {
    fillSnippet(panel, 'curl');
  }
}

function switchProviderOS(btn, os) {
  var root = btn.closest('[data-testid=provider-os-setup]');
  if (!root) return;
  root.querySelectorAll('[data-testid=provider-os-tabs] button').forEach(function (b) {
    b.classList.remove('on');
  });
  btn.classList.add('on');
  root.querySelectorAll('[data-os-panel]').forEach(function (panel) {
    if (panel.getAttribute('data-os-panel') === os) {
      panel.classList.remove('hidden');
    } else {
      panel.classList.add('hidden');
    }
  });
}

// Legacy name used by older markup / tests.
function toggleSnippets(btn) {
  toggleSetup(btn);
}

function switchSnippet(btn, kind) {
  var panel = btn.closest('[data-testid=setup-panel]') || btn.closest('[data-testid=snippets-panel]');
  if (!panel) return;
  panel.querySelectorAll('.snippet-tabs button').forEach(function (b) { b.classList.remove('on'); });
  btn.classList.add('on');
  fillSnippet(panel, kind);
}

function buildSnippet(kind, baseURL, model) {
  baseURL = baseURL || 'https://gpumesh.net/v1/machines/mch_…';
  model = model || 'llama3.2:3b';
  var apiKey = resolveApiKey();
  if (kind === 'cursor') {
    return '# Cursor → Settings → Models\n' +
      '# OpenAI API → enable + Override Base URL\n' +
      'Override OpenAI Base URL: ' + baseURL + '\n' +
      'OpenAI API Key: ' + apiKey + '\n' +
      'Add model: ' + model;
  }
  if (kind === 'cline') {
    return 'cline auth \\\n' +
      '  -p openai-compatible \\\n' +
      '  -k \'' + apiKey + '\' \\\n' +
      '  -m \'' + model + '\' \\\n' +
      '  -b \'' + baseURL + '\'';
  }
  if (kind === 'pi') {
    return '# ~/.pi/agent/models.json — merge into providers\n' +
      '{\n' +
      '  "providers": {\n' +
      '    "gpumesh": {\n' +
      '      "baseUrl": "' + baseURL + '",\n' +
      '      "api": "openai-completions",\n' +
      '      "apiKey": "' + apiKey + '",\n' +
      '      "models": [{ "id": "' + model + '" }]\n' +
      '    }\n' +
      '  }\n' +
      '}\n' +
      '\n' +
      'pi --provider gpumesh --model ' + model;
  }
  if (kind === 'python') {
    return 'from openai import OpenAI\nclient = OpenAI(\n  base_url="' + baseURL + '",\n  api_key="' + apiKey + '",\n)\n' +
      'client.chat.completions.create(\n  model="' + model + '",\n  messages=[{"role":"user","content":"hi"}],\n)';
  }
  return 'export OPENAI_BASE_URL="' + baseURL + '"\n' +
    'export OPENAI_API_KEY="' + apiKey + '"\n' +
    'curl "$OPENAI_BASE_URL/chat/completions" \\\n' +
    '  -H "Authorization: Bearer $OPENAI_API_KEY" \\\n' +
    '  -d \'{"model":"' + model + '","messages":[{"role":"user","content":"hi"}]}\'';
}

function copySnippet(btn) {
  var panel = btn.closest('[data-testid=setup-panel]') || btn.closest('[data-testid=snippets-panel]');
  if (!panel) return;
  var code = panel.querySelector('[data-testid=snippet-code]');
  if (!code) return;
  copyText(btn, code.textContent);
}

// Keep legacy names used by older markup.
function switchTab() {}
function toggleAccordion(el) { if (el) el.classList.toggle('open'); }
function toggleModel() {}
function toggleTool() {}
function dismissTryNow() {}
function copyOmpCmd() {}

/**
 * Preserve UI state across HTMX panel polls and skip no-op swaps.
 * Full innerHTML replace every ~10s makes buttons jump/flicker
 * even when the payload is identical (SPEC-v2 §9.1).
 */
(function () {
  var saved = { details: {}, fields: {}, snippets: {}, snippetTab: {}, providerOs: 'unix' };

  function isPollTarget(el) {
    if (!el) return false;
    if (el.id === 'use-machines' || el.id === 'share-panel') return true;
    var tid = el.getAttribute('data-testid');
    return tid === 'members-section' || tid === 'use-machines' || tid === 'share-panel';
  }

  function setupPanel(card) {
    return card.querySelector('[data-testid=setup-panel]') || card.querySelector('[data-testid=snippets-panel]');
  }

  function capture(root) {
    if (!root || !root.querySelectorAll) return;
    root.querySelectorAll('details[data-testid]').forEach(function (el) {
      saved.details[el.getAttribute('data-testid')] = el.open;
    });
    root.querySelectorAll('select[name], input[name]').forEach(function (el) {
      if (!el.name) return;
      saved.fields[el.name] = el.value;
    });
    root.querySelectorAll('[data-testid=machine-card]').forEach(function (card) {
      var id = card.getAttribute('data-machine-id');
      if (!id) return;
      var panel = setupPanel(card);
      if (panel) saved.snippets[id] = !panel.classList.contains('hidden');
      var on = panel && panel.querySelector('.snippet-tabs button.on');
      if (on) saved.snippetTab[id] = on.getAttribute('data-snippet') || 'curl';
    });
    var osOn = root.querySelector('[data-testid=provider-os-tabs] button.on');
    if (osOn) saved.providerOs = osOn.getAttribute('data-os') || 'unix';
  }

  function restore(root) {
    if (!root || !root.querySelectorAll) return;
    root.querySelectorAll('details[data-testid]').forEach(function (el) {
      var id = el.getAttribute('data-testid');
      if (saved.details[id]) el.open = true;
    });
    root.querySelectorAll('select[name], input[name]').forEach(function (el) {
      if (!el.name || saved.fields[el.name] === undefined) return;
      el.value = saved.fields[el.name];
    });
    root.querySelectorAll('[data-testid=machine-card]').forEach(function (card) {
      var id = card.getAttribute('data-machine-id');
      var panel = setupPanel(card);
      if (!id || !panel) return;
      var open = Object.prototype.hasOwnProperty.call(saved.snippets, id)
        ? saved.snippets[id]
        : !panel.classList.contains('hidden');
      if (!open) {
        panel.classList.add('hidden');
        return;
      }
      panel.classList.remove('hidden');
      var kind = saved.snippetTab[id] || 'curl';
      var tab = panel.querySelector('.snippet-tabs button[data-snippet="' + kind + '"]');
      if (tab) {
        panel.querySelectorAll('.snippet-tabs button').forEach(function (b) { b.classList.remove('on'); });
        tab.classList.add('on');
      }
      fillSnippet(panel, kind);
    });
    if (saved.providerOs && saved.providerOs !== 'unix') {
      var osTab = root.querySelector('[data-testid=provider-os-tabs] button[data-os="' + saved.providerOs + '"]');
      if (osTab) switchProviderOS(osTab, saved.providerOs);
    }
  }

  document.body.addEventListener('htmx:beforeSwap', function (evt) {
    var target = evt.detail && evt.detail.target;
    capture(target);
    if (!isPollTarget(target)) return;
    var next = evt.detail.serverResponse;
    if (typeof next !== 'string') return;
    if (target.dataset.lastPollHtml === next) {
      // Identical poll payload — keep live DOM (avoids Copy base URL jump).
      evt.detail.shouldSwap = false;
      return;
    }
    target.dataset.lastPollHtml = next;
  });
  document.body.addEventListener('htmx:afterSwap', function (evt) {
    restore(evt.detail && evt.detail.target);
  });
})();
