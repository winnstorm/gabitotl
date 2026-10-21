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
  patchApp(html);
  pane = document.getElementById('pane');
  var key = pageKey();
  if (pane) pane.scrollTop = key === lastPage ? scroll : 0;
  var log = document.querySelector && document.querySelector('.log');
  if (log) log.scrollTop = log.scrollHeight;
  if (active) {
    var el = document.querySelector('[data-k="' + active.replace(/"/g, '\\"') + '"]');
    if (el && !el.disabled) el.focus();
  }
  var akey = key + (S.m && S.m.busy && S.info && S.info.route !== 'wizard' ? ':loading' : '');
  var dir = S.dir;
  if (akey !== lastAnim) S.dir = '';
  lastPage = key;
  lastAnim = akey;
  UI.afterRender(akey, dir, S.m && S.m.modal, S.m && S.m.toast);
  if (S.info && S.info.route !== 'wizard') managerAfterRender();
}

// patchApp swaps the new markup into #app but keeps the title bar and the footer
// nodes when their markup did not change. A click is a mousedown and a mouseup on
// the same node: replacing the footer in between (a progress event, a late reply)
// used to swallow the click on Install.
var KEEP = ['.tb', '.ft'];
function patchApp(html) {
  var app = document.getElementById('app');
  if (typeof document.createElement !== 'function' || !app.replaceChildren) { app.innerHTML = html; return; }
  var tpl = document.createElement('template');
  tpl.innerHTML = html;
  var oldKids = Array.prototype.slice.call(app.children);
  var newKids = Array.prototype.slice.call(tpl.content.children);
  if (oldKids.length !== newKids.length) { app.replaceChildren(tpl.content); newKids.forEach(sign); return; }
  // Same layout: replace child by child and leave an unchanged .tb/.ft exactly
  // where it is. Detaching and re-attaching a node cancels a click in progress.
  newKids.forEach(function (fresh, i) {
    var old = oldKids[i];
    var sig = fresh.outerHTML;
    if (KEEP.some(function (sel) { return fresh.matches(sel); }) && old._sig === sig) return;
    fresh._sig = sig;
    old.replaceWith(fresh);
  });
}

function sign(n) { n._sig = n.outerHTML; }

// pageKey names the screen on view: a change slides or fades the page in.
var lastPage = null, lastAnim = null;
function pageKey() {
  if (!S.info) return 'boot';
  if (S.info.route === 'wizard') return 'w' + S.step + (S.finished && !S.installErr ? 'd' : '');
  return 'm:' + S.m.screen + ':' + S.m.proj + ':' + (S.m.screen === 'project' ? S.m.tab : '');
}

// ---- small view helpers -----------------------------------------------------

function attr(name, val, on) { return on ? ' ' + name + (val === true ? '' : '="' + esc(val) + '"') : ''; }

function check(path, label, o) {
  o = o || {};
  return '<label class="chk' + (o.disabled ? ' off' : '') + '"' + attr('title', o.title, !!o.title) + '>' +
    '<input type="checkbox" data-bind="' + path + '" data-k="' + path + '"' + attr('checked', true, !!get(path)) + attr('disabled', true, !!o.disabled) + '>' +
    esc(label) + '</label>';
}

// radio is an option card: icon, title, one line, a selected state and, when o.pop
// names an explanation, the popover (hover, focus or the (i) button). o: disabled,
// icon, pop.
function radio(path, val, name, desc, o) {
  o = o || {};
  var on = get(path) === val;
  return '<div class="opt' + (on ? ' sel' : '') + (o.disabled ? ' off' : '') + '"' + UI.popAttr(o.pop) + ' data-pk="' + esc(path + '-' + val) + '"><label>' +
    '<input type="radio" name="' + path + '" value="' + esc(val) + '" data-bind="' + path + '" data-k="' + path + '-' + esc(val) + '"' + attr('checked', true, on) + attr('disabled', true, !!o.disabled) + '>' +
    (o.icon ? '<span class="oic">' + UI.icon(o.icon, 'lg') + '</span>' : '') +
    '<div><b>' + esc(name) + '</b>' + (desc ? '<span>' + esc(desc) + '</span>' : '') + '</div></label>' + UI.infoBtn(o.pop) + '</div>';
}

// phead is the header of a screen: an icon tile, the title and a sentence. o: id
// (the title becomes the focus target of the Manager), step (the wizard's "Step n of N").
function phead(ic, title, lead, o) {
  o = o || {};
  var of = o.step ? '<div class="stepof">' + esc(t('gui.wiz.stepof', { n: S.step + 1, total: STEPS.length })) + '</div>' : '';
  return '<div class="phead"><span class="pico">' + UI.icon(ic, 'lg') + '</span><div>' + of + '<h1' + (o.id ? ' id="' + o.id + '" tabindex="-1"' : '') + '>' + esc(title) + '</h1>' + (lead ? '<p class="lead">' + esc(lead) + '</p>' : '') + '</div></div>';
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

// rtIcon is the generic glyph of an agent (not its logo).
function rtIcon(name) { return { 'claude-code': 'spark', opencode: 'terminal', kiro: 'ghost' }[name] || 'bot'; }

var BADGE_ICON = { ok: 'check', warn: 'warn', todo: 'info', err: 'error' };

function badge(cls, text, ic) {
  var name = ic || BADGE_ICON[cls];
  return '<span class="badge ' + cls + '">' + (name ? UI.icon(name, 'sm') : '') + esc(text) + '</span>';
}

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
  if (el.type === 'radio' || el.type === 'checkbox') UI.pulse(el.getAttribute('data-k'));
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
