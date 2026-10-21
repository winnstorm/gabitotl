'use strict';
// app.js is the shell of the desktop app: state, the Go bridge, rendering and
// event delegation. The wizard screens live in wizard.js. There is no framework:
// each change re-renders the screen from the state object S.

var api = function () { return window.go.main.App; };

var S = {
  info: null,
  lang: 'es',
  step: 0,
  error: '',
  root: '',
  detect: null,
  detectErr: '',
  scanning: false,
  cred: { list: [], servers: [], srv: { platform: 'bitbucket-server', url: '', tok: '', ssh: '', conflict: false }, err: '', busy: '', tok: { github: '', gitlab: '', bitbucket: '' }, msg: {} },
  cg: null,
  a: {
    license: false,
    gentleAction: '',
    name: '',
    personaMode: 'normal',
    runtimes: {},
    migratePersona: true,
    retireGentleKiro: true,
    memory: { mode: 'solo', role: 'host', engine: 'auto', port: '', instance: '', url: '', tokenEnv: '', importEngram: false },
    delivery: { automation: 'until-pr', mergeMethod: 'squash', deleteBranch: true, draft: true },
    win: { installDir: '', startMenu: true, desktopShortcut: false, addToPath: true },
  },
  plan: null,
  planErr: '',
  running: false,
  finished: false,
  installErr: '',
  result: null,
  pct: 0,
  progressMsg: '',
  log: [],
};

function esc(s) {
  return String(s === undefined || s === null ? '' : s).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

function get(path) {
  return path.split('.').reduce(function (o, k) { return o[k]; }, S);
}

function set(path, val) {
  var keys = path.split('.');
  var last = keys.pop();
  keys.reduce(function (o, k) { return o[k]; }, S)[last] = val;
}

function errText(e) {
  return String((e && e.message) || e);
}

// ---- rendering --------------------------------------------------------------

var LOGO = '<svg width="%s" height="%s" viewBox="0 0 64 64" aria-hidden="true"><rect width="64" height="64" rx="14" fill="#0f172a"/><rect x="14" y="20" width="36" height="28" rx="8" fill="#38bdf8"/><circle cx="25" cy="34" r="4" fill="#0f172a"/><circle cx="39" cy="34" r="4" fill="#0f172a"/><rect x="30" y="11" width="4" height="9" rx="2" fill="#38bdf8"/><circle cx="32" cy="10" r="3" fill="#fbbf24"/><rect x="24" y="42" width="16" height="3" rx="1.5" fill="#0f172a"/></svg>';

function logo(size) { return LOGO.replace(/%s/g, size); }

function titlebar(titleKey) {
  return '<div class="tb">' + logo(16) + '<span>' + esc(t(titleKey || 'gui.app.title')) + '</span><span class="sp"></span>' +
    '<div class="langsw" role="group" aria-label="' + esc(t('gui.lang.switch')) + '">' +
    ['es', 'en'].map(function (l) {
      return '<button type="button" data-act="lang" data-val="' + l + '" data-k="lang-' + l + '" aria-pressed="' + (S.lang === l) + '">' + l.toUpperCase() + '</button>';
    }).join('') + '</div></div>';
}

function render() {
  var active = document.activeElement && document.activeElement.getAttribute && document.activeElement.getAttribute('data-k');
  var pane = document.getElementById('pane');
  var scroll = pane ? pane.scrollTop : 0;
  var html;
  switch (S.info && S.info.route) {
    case 'wizard': html = renderWizard(); break;
    case 'manage':
    case 'uninstall': html = renderManager(); break;
    default: html = titlebar() + '<div class="body"><main class="pane center" id="pane">' + esc(t('gui.loading')) + '</main></div>';
  }
  document.getElementById('app').innerHTML = html;
  pane = document.getElementById('pane');
  if (pane) pane.scrollTop = scroll;
  var log = document.querySelector && document.querySelector('.log');
  if (log) log.scrollTop = log.scrollHeight;
  if (active) {
    var el = document.querySelector('[data-k="' + active.replace(/"/g, '\\"') + '"]');
    if (el && !el.disabled) el.focus();
  }
  if (S.info && S.info.route !== 'wizard') managerAfterRender();
}

// ---- small view helpers -----------------------------------------------------

function attr(name, val, on) { return on ? ' ' + name + (val === true ? '' : '="' + esc(val) + '"') : ''; }

function check(path, label, o) {
  o = o || {};
  return '<label class="chk' + (o.disabled ? ' off' : '') + '"' + attr('title', o.title, !!o.title) + '>' +
    '<input type="checkbox" data-bind="' + path + '" data-k="' + path + '"' + attr('checked', true, !!get(path)) + attr('disabled', true, !!o.disabled) + '>' +
    esc(label) + '</label>';
}

function radio(path, val, name, desc, o) {
  o = o || {};
  var on = get(path) === val;
  return '<label class="opt' + (on ? ' sel' : '') + (o.disabled ? ' off' : '') + '">' +
    '<input type="radio" name="' + path + '" value="' + esc(val) + '" data-bind="' + path + '" data-k="' + path + '-' + esc(val) + '"' + attr('checked', true, on) + attr('disabled', true, !!o.disabled) + '>' +
    '<div><b>' + esc(name) + '</b>' + (desc ? '<span>' + esc(desc) + '</span>' : '') + '</div></label>';
}

// seg draws a button group; items are [value, catalogueKey, disabled?].
function seg(path, items, label) {
  return '<div class="seg" role="group" aria-label="' + esc(label) + '">' + items.map(function (it) {
    return '<button type="button" data-act="set" data-path="' + path + '" data-val="' + esc(it[0]) + '" data-k="' + path + '-' + esc(it[0]) + '" aria-pressed="' + (get(path) === it[0]) + '"' + attr('disabled', true, !!it[2]) + '>' + esc(t(it[1])) + '</button>';
  }).join('') + '</div>';
}

function field(path, label, o) {
  o = o || {};
  return '<label class="field"' + (o.style ? ' style="' + o.style + '"' : '') + '>' + esc(label) +
    '<input' + (o.mono ? ' class="mono"' : '') + ' type="' + (o.type || 'text') + '" data-bind="' + path + '" data-k="' + path + '" data-norender value="' + esc(get(path)) + '"' +
    attr('placeholder', o.placeholder, !!o.placeholder) + attr('disabled', true, !!o.disabled) + '>' +
    (o.hint ? '<span class="hint">' + esc(o.hint) + '</span>' : '') + '</label>';
}

function badge(cls, text) { return '<span class="badge ' + cls + '">' + esc(text) + '</span>'; }

// ---- events -----------------------------------------------------------------

var actions = {};

document.addEventListener('click', function (ev) {
  var el = ev.target.closest && ev.target.closest('[data-act]');
  if (!el || el.disabled) return;
  var fn = actions[el.getAttribute('data-act')];
  if (fn) fn(el, ev);
});

document.addEventListener('input', function (ev) {
  var el = ev.target;
  var path = el.getAttribute && el.getAttribute('data-bind');
  if (path && el.hasAttribute('data-norender')) set(path, el.value);
});

document.addEventListener('change', function (ev) {
  var el = ev.target;
  var path = el.getAttribute && el.getAttribute('data-bind');
  if (!path) return;
  if (el.type === 'checkbox') set(path, el.checked);
  else set(path, el.value);
  if (!el.hasAttribute('data-norender')) render();
});

actions.set = function (el) {
  set(el.getAttribute('data-path'), el.getAttribute('data-val'));
  render();
};

actions.quit = function () { api().Quit(); };

actions.lang = function (el) { switchLang(el.getAttribute('data-val')); };

async function switchLang(l) {
  if (l === S.lang || S.langBusy) return;
  S.langBusy = true;
  try {
    await api().SetLanguage(l);
    I18n.set(await api().Messages(l));
    S.lang = l;
    S.error = '';
    if (S.info && S.info.route !== 'wizard') { S.langBusy = false; await managerReload(); }
  } catch (e) {
    S.error = t('gui.error.generic', { detail: errText(e) });
  }
  S.langBusy = false;
  render();
}

async function boot() {
  try {
    S.info = await api().Info();
    try { S.lang = (await api().Language()) || 'es'; } catch (e) { S.lang = 'es'; }
    I18n.set(await api().Messages(S.lang));
    S.a.win.installDir = S.info.installDir;
    S.root = '';
    if (S.info.route !== 'wizard') await managerBoot();
    if (window.runtime && window.runtime.EventsOn) { window.runtime.EventsOn('install:progress', onProgress); window.runtime.EventsOn('op:progress', onOpProgress); }
    render();
  } catch (e) {
    document.getElementById('app').textContent = errText(e);
  }
}

window.addEventListener('DOMContentLoaded', boot);
