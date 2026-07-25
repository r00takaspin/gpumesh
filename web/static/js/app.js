// GPU Mesh — JS helpers (vanilla, no framework)

/**
 * Copy text from adjacent <pre> or data-copy attribute to clipboard.
 * Adds a green flash animation on the source code block.
 */
function copyToClipboard(btn) {
  let text = btn.getAttribute('data-copy');
  let codeBlock = null;

  if (!text) {
    // Fallback: grab text from adjacent <pre><code>
    const card = btn.closest('.consumer-card, .card');
    if (card) {
      const pre = card.querySelector('pre');
      if (pre) {
        const code = pre.querySelector('code');
        text = code ? code.textContent : pre.textContent;
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
  if (codeBlock) {
    codeBlock.classList.add('flash-copied');
  }
  setTimeout(function () {
    btn.textContent = orig;
    btn.classList.remove('copied');
    if (codeBlock) {
      codeBlock.classList.remove('flash-copied');
    }
  }, 2000);
}

/**
 * Switch between consumer tabs (.consumer-tab + .tab-panel).
 */
function switchTab(tab, el) {
  document.querySelectorAll('.consumer-tab').forEach(function (t) { t.classList.remove('active'); });
  document.querySelectorAll('.tab-panel').forEach(function (p) { p.classList.remove('active'); });
  if (el) el.classList.add('active');
  var panel = document.getElementById('tab-' + tab);
  if (panel) panel.classList.add('active');
  var url = new URL(window.location);
  url.searchParams.set('tab', tab);
  history.replaceState(null, '', url);
}

/**
 * Toggle an accordion item open/closed.
 */
function toggleAccordion(el) {
  el.classList.toggle('open');
}

/**
 * Dismiss the one-time API key display.
 */
function dismissKey() {
  var el = document.getElementById('onetime-key');
  if (el) el.style.display = 'none';
  var w = document.getElementById('onetime-warning');
  if (w) w.style.display = 'none';
}

/**
 * Dismiss the "Try it now" block (persisted in localStorage).
 */
function dismissTryNow() {
  var el = document.getElementById('try-now');
  if (el) { el.style.display = 'none'; localStorage.setItem('try-now-dismissed', '1'); }
}

// Show "Try it now" if not previously dismissed.
(function () {
  if (localStorage.getItem('try-now-dismissed') !== '1') {
    var el = document.getElementById('try-now');
    if (el) el.style.display = '';
  }
})();

/**
 * Toggle a model card's config section open/closed.
 */
function toggleModel(head) {
  var configs = head.parentElement.querySelector('.model-configs');
  if (configs) configs.classList.toggle('open');
}

/**
 * Toggle a tool row open/closed (shows code snippet below).
 */
function toggleTool(row, evt) {
  row.classList.toggle('open');
}

/**
 * Core copy helper — fallback for older browsers.
 */
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
  setTimeout(function () { btn.textContent = 'Copy'; }, 2000);
}

/**
 * Copy the code from the nearest <pre> block.
 */
function copyCode(btn) {
  var pre = btn.closest('pre');
  if (!pre) return;
  var code = pre.querySelector('code');
  var text = code ? code.textContent : pre.textContent;
  doCopy(btn, text.trim());
}

/**
 * Copy arbitrary text.
 */
function copyText(btn, text) {
  doCopy(btn, text);
}

/**
 * Copy an API key.
 */
function copyKey(btn, key) {
  doCopy(btn, key);
}

/**
 * Copy the Oh My Pi launch command.
 */
function copyOmpCmd(btn) {
  var model = btn.getAttribute('data-model');
  doCopy(btn, 'omp run "write a hello world" --model ' + model);
}
