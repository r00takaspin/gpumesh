// GPU Mesh — JS helpers (vanilla, no framework)

/**
 * Copy text from adjacent <pre> or data-copy attribute to clipboard.
 * Adds a green flash animation on the source code block.
 */
function copyToClipboard(btn) {
  let text = btn.getAttribute('data-copy');
  let codeBlock = null;

  if (!text) {
    const wrapper = btn.closest('.code-wrapper');
    if (wrapper) {
      codeBlock = wrapper.querySelector('pre.code-block');
      if (codeBlock) text = codeBlock.textContent;
    }
    if (!text) {
      const prev = btn.previousElementSibling;
      if (prev && prev.tagName === 'PRE') {
        codeBlock = prev;
        text = prev.textContent;
      }
    }
    if (!text && btn.parentElement) {
      const prev = btn.parentElement.previousElementSibling;
      if (prev && prev.tagName === 'PRE') {
        codeBlock = prev;
        text = prev.textContent;
      }
    }
  }

  if (!text) return;

  navigator.clipboard.writeText(text.trim()).then(() => {
    flashCopied(btn, codeBlock);
  }).catch(() => {
    const ta = document.createElement('textarea');
    ta.value = text.trim();
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    document.body.removeChild(ta);
    flashCopied(btn, codeBlock);
  });
}

function flashCopied(btn, codeBlock) {
  const orig = btn.textContent;
  btn.textContent = 'Copied!';
  btn.classList.add('copied');

  if (codeBlock) {
    codeBlock.classList.add('flash-copied');
    setTimeout(() => codeBlock.classList.remove('flash-copied'), 600);
  }

  setTimeout(() => {
    btn.textContent = orig;
    btn.classList.remove('copied');
  }, 2000);
}

/**
 * Switch between tabs in a tab group.
 */
function switchTab(tabGroup, tabName) {
  document.querySelectorAll(`.tool-tab-btn[data-tab-group="${tabGroup}"]`).forEach(btn => {
    btn.classList.toggle('active', btn.getAttribute('data-tab') === tabName);
  });
  document.querySelectorAll(`.tab-panel[data-tab-group="${tabGroup}"]`).forEach(panel => {
    panel.classList.toggle('active', panel.getAttribute('data-tab') === tabName);
  });
}

/**
 * Toggle an accordion item open/closed.
 */
function toggleAccordion(el) {
  var e = window.event;
  if (e) {
    var target = e.target || e.srcElement;
    if (target) {
      var tag = target.tagName;
      if (tag === 'BUTTON' || tag === 'A' || tag === 'INPUT') return;
    }
  }
  el.classList.toggle('open');
}


// Activate tab from URL hash on page load.
document.addEventListener('DOMContentLoaded', () => {
  const hash = window.location.hash.slice(1); // remove '#'
  if (hash === 'consumer' || hash === 'donor') {
    switchTab('dash', hash);
  }
  // Also handle clicks on tab buttons — update hash.
  document.querySelectorAll('.tab-btn[data-tab-group="dash"]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = btn.getAttribute('data-tab');
      if (tab) {
        history.replaceState(null, '', '#' + tab);
      }
    });
  });
});
