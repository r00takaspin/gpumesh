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
  document.querySelectorAll(`.tab-btn[data-tab-group="${tabGroup}"]`).forEach(btn => {
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
  el.classList.toggle('open');
}

// ── Hero Command Line ──

const COMMANDS = {
  'get-api-key':   { url: '/dashboard.html',  desc: 'Generate an API key for the mesh' },
  'donate':        { url: '/dashboard.html',  desc: 'Share your GPU with the mesh' },
  'status':        { url: '/status.html',     desc: 'Check system status' },
  'models':        { url: '/models.html',     desc: 'Browse available models' },
  'leaderboard':   { url: '/leaderboard.html',desc: 'View donor leaderboard' },
  'help':          { url: null,               desc: 'Show this help' },
  'dashboard':     { url: '/dashboard.html',  desc: 'Open your dashboard' },
  'home':          { url: '/index.html',      desc: 'Go to landing page' },
};

document.addEventListener('DOMContentLoaded', function() {
  const input = document.getElementById('hero-cmd');
  const suggestions = document.getElementById('cli-suggestions');
  if (!input || !suggestions) return;

  input.addEventListener('input', function() {
    const val = this.value.trim().toLowerCase();
    const matches = Object.keys(COMMANDS).filter(cmd => cmd.startsWith(val));

    if (val === '') {
      suggestions.innerHTML = '<span class="cli-hint">Available: get-api-key, donate, status, models, leaderboard, help</span>';
      suggestions.classList.remove('active');
    } else if (matches.length === 1 && matches[0] === val) {
      suggestions.innerHTML = `<span class="cli-match">${matches[0]}</span> <span class="cli-desc">— ${COMMANDS[matches[0]].desc}</span>`;
      suggestions.classList.add('active');
    } else if (matches.length > 0) {
      suggestions.innerHTML = matches.map(m =>
        `<span class="cli-match${m === val ? ' exact' : ''}">${m}</span>`
      ).join(' ');
      suggestions.classList.add('active');
    } else {
      suggestions.innerHTML = '<span class="cli-hint text-error">command not found: ' + this.value.trim() + '</span>';
      suggestions.classList.add('active');
    }
  });

  input.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') {
      const val = this.value.trim().toLowerCase();
      const cmd = COMMANDS[val];
      if (cmd && cmd.url) {
        window.location.href = cmd.url;
      } else if (val === 'help') {
        this.value = '';
        const list = Object.entries(COMMANDS).map(([k,v]) => `${k} — ${v.desc}`).join('<br>');
        suggestions.innerHTML = `<span class="cli-hint">${list}</span>`;
        suggestions.classList.add('active');
      }
    } else if (e.key === 'Tab') {
      e.preventDefault();
      const val = this.value.trim().toLowerCase();
      const matches = Object.keys(COMMANDS).filter(cmd => cmd.startsWith(val));
      if (matches.length === 1) {
        this.value = matches[0];
        this.dispatchEvent(new Event('input'));
      }
    }
  });
});
