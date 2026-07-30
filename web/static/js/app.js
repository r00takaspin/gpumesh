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

function toggleSnippets(btn) {
  var card = btn.closest('[data-testid=machine-card]');
  if (!card) return;
  var panel = card.querySelector('[data-testid=snippets-panel]');
  if (!panel) return;
  panel.classList.toggle('hidden');
  var code = panel.querySelector('[data-testid=snippet-code]');
  if (code && !code.textContent.trim()) {
    code.textContent = buildSnippet('curl', code.getAttribute('data-base-url'), code.getAttribute('data-model') || 'llama3.2:3b');
  }
}

function switchSnippet(btn, kind) {
  var panel = btn.closest('[data-testid=snippets-panel]');
  if (!panel) return;
  panel.querySelectorAll('.snippet-tabs button').forEach(function (b) { b.classList.remove('on'); });
  btn.classList.add('on');
  var code = panel.querySelector('[data-testid=snippet-code]');
  if (!code) return;
  code.textContent = buildSnippet(kind, code.getAttribute('data-base-url'), code.getAttribute('data-model') || 'llama3.2:3b');
}

function buildSnippet(kind, baseURL, model) {
  baseURL = baseURL || 'https://gpumesh.net/v1/machines/mch_…';
  model = model || 'llama3.2:3b';
  if (kind === 'continue') {
    return 'models:\n  - title: machine\n    provider: openai\n    model: ' + model +
      '\n    apiBase: ' + baseURL + '\n    apiKey: inf_…';
  }
  if (kind === 'cline') {
    return 'OpenAI Compatible\nBase URL: ' + baseURL + '\nAPI Key: inf_…\nModel: ' + model;
  }
  if (kind === 'python') {
    return 'from openai import OpenAI\nclient = OpenAI(\n  base_url="' + baseURL + '",\n  api_key="inf_…",\n)\n' +
      'client.chat.completions.create(\n  model="' + model + '",\n  messages=[{"role":"user","content":"hi"}],\n)';
  }
  return 'export OPENAI_BASE_URL="' + baseURL + '"\n' +
    'export OPENAI_API_KEY="inf_…"\n' +
    'curl "$OPENAI_BASE_URL/chat/completions" \\\n' +
    '  -H "Authorization: Bearer $OPENAI_API_KEY" \\\n' +
    '  -d \'{"model":"' + model + '","messages":[{"role":"user","content":"hi"}]}\'';
}

function copySnippet(btn) {
  var panel = btn.closest('[data-testid=snippets-panel]');
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
 * Preserve <details open> and form field values across HTMX panel polls
 * (e.g. /share/panel every 10s would otherwise collapse sections).
 */
(function () {
  var saved = { details: {}, fields: {}, snippets: {} };

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
      var panel = card.querySelector('[data-testid=snippets-panel]');
      if (id && panel) saved.snippets[id] = !panel.classList.contains('hidden');
    });
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
      var panel = card.querySelector('[data-testid=snippets-panel]');
      if (!id || !panel || !saved.snippets[id]) return;
      panel.classList.remove('hidden');
      var code = panel.querySelector('[data-testid=snippet-code]');
      if (code && !code.textContent.trim()) {
        code.textContent = buildSnippet(
          'curl',
          code.getAttribute('data-base-url'),
          code.getAttribute('data-model') || 'llama3.2:3b'
        );
      }
    });
  }

  document.body.addEventListener('htmx:beforeSwap', function (evt) {
    capture(evt.detail && evt.detail.target);
  });
  document.body.addEventListener('htmx:afterSwap', function (evt) {
    restore(evt.detail && evt.detail.target);
  });
})();
