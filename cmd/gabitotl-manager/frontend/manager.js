'use strict';
// manager.js: the GabitoTL Manager (Overview, Doctor, Integrations, Persona,
// Memory, Delivery, Guard & code index, Backups, Uninstall). Every change goes
// through planAndRun: the plan is shown in a modal before anything is applied.

// Two levels. Global: what applies to every project (delivery defaults and
// credentials, CodeGraph, agents, persona, Windows, backups, uninstall). Projects:
// the folders GabitoTL or your agents know, each with its own page (overview,
// delivery override, code index, guard hooks, memory, doctor, agents, backups).
// The install is always global; a project is only a folder you open.

var GLOBAL_NAV = [
  ['overview', 'gui.mgr.nav.overview', 'home'],
  ['delivery', 'gui.mgr.nav.delivery-global', 'pr'],
  ['codegraph', 'gui.mgr.nav.codegraph', 'graph'],
  ['integrations', 'gui.mgr.nav.integrations', 'plug'],
  ['persona', 'gui.mgr.nav.persona', 'chat'],
  ['doctor', 'gui.mgr.nav.doctor', 'pulse'],
  ['windows', 'gui.mgr.nav.windows', 'desktop'],
  ['backups', 'gui.mgr.nav.backups', 'archive'],
  ['uninstall', 'gui.mgr.nav.uninstall', 'trash'],
];

// Project tabs; needs: the tab only works once GabitoTL is set up in the folder.
var PROJECT_TABS = [
  ['overview', 'gui.mgr.nav.overview', false, 'home'],
  ['delivery', 'gui.mgr.nav.delivery', true, 'pr'],
  ['index', 'gui.proj.tab.index', false, 'graph'],
  ['guard', 'gui.proj.tab.guard', true, 'shield'],
  ['memory', 'gui.mgr.nav.memory', true, 'db'],
  ['doctor', 'gui.mgr.nav.doctor', true, 'pulse'],
  ['integrations', 'gui.mgr.nav.integrations', true, 'plug'],
  ['backups', 'gui.mgr.nav.backups', true, 'archive'],
];

// The sources a project was found in, and the platform of its remote, as icons.
var SRC_ICON = { gabitotl: 'bot', 'claude-code': 'spark', opencode: 'terminal', kiro: 'ghost' };

var SRC_KEYS = { gabitotl: 'gui.projects.src.gabitotl', 'claude-code': 'gui.projects.src.claude-code', opencode: 'gui.projects.src.opencode', kiro: 'gui.projects.src.kiro' };

S.m = {
  screen: 'overview', tab: 'overview', proj: '', busy: false,
  doctor: null, integrations: null, settings: null, snaps: null,
  gd: null, cg: null, eng: null, projects: null, pd: null,
  seq: 0, toast: '', modal: null, loadErr: '', focusTitle: false,
  pf: '', psrc: 'all',
  f: {
    lang: 'es', mode: 'normal',
    mem: { mode: 'solo', role: 'host', engine: 'auto', port: '', instance: '', url: '', tokenEnv: '', importEngram: false },
    gdel: { automation: 'until-pr', mergeMethod: 'squash', deleteBranch: true, draft: true },
    pdel: { automation: 'until-pr', mergeMethod: 'squash', deleteBranch: true, draft: true },
  },
  un: { runtimes: {}, guard: true, program: true, server: false },
  purge: '',
};

// PR is the project folder the current screen works on: the open project on a
// project page, "" (global) everywhere else.
function PR() { return S.m.screen === 'project' ? S.m.proj : ''; }

async function managerBoot() {
  try { S.root = (await api().Root()) || ''; } catch (e) { S.root = ''; }
  S.m.screen = S.info.route === 'uninstall' ? 'uninstall' : 'overview';
  await loadScreen();
}

async function managerReload() { await loadScreen(); }

function projectName() {
  if (!S.m.proj) return t('gui.mgr.project.none');
  return baseName(S.m.proj);
}

function baseName(p) {
  var parts = String(p).replace(/[\\/]+$/, '').split(/[\\/]/);
  return parts[parts.length - 1] || p;
}

// ---- data loading -------------------------------------------------------------

// soft runs one read; a failure leaves the fallback so one broken source never
// blanks a whole screen.
async function soft(fn, fallback) {
  try { return await fn(); } catch (e) { return fallback; }
}

// keep returns a loaded value, or the previous one when a newer load started
// meanwhile, so a slow answer for a project you left never overwrites the page.
function keep(seq, value, old) { return seq === S.m.seq ? value : old; }

async function loadScreen() {
  var seq = ++S.m.seq;
  S.m.loadErr = '';
  S.m.busy = true;
  render();
  try {
    var sc = S.m.screen;
    var r = PR();
    if (sc === 'overview') {
      S.m.doctor = keep(seq, await soft(function () { return api().Doctor(''); }, null), S.m.doctor);
      S.m.integrations = keep(seq, await soft(function () { return api().Integrations(''); }, []), S.m.integrations);
      S.m.settings = keep(seq, await soft(function () { return api().Settings(''); }, null), S.m.settings);
      S.m.cg = keep(seq, await soft(function () { return api().CodeGraph(); }, null), S.m.cg);
      S.m.gd = keep(seq, await soft(function () { return api().GlobalDelivery(); }, null), S.m.gd);
      S.m.projects = keep(seq, await soft(function () { return api().Projects(); }, []), S.m.projects);
      await loadCredsQuiet();
    } else if (sc === 'delivery') {
      S.m.gd = keep(seq, await api().GlobalDelivery(), S.m.gd);
      seedGlobalDelivery(S.m.gd);
      await loadCredsQuiet();
    } else if (sc === 'codegraph') {
      S.m.cg = keep(seq, await api().CodeGraph(), S.m.cg);
    } else if (sc === 'integrations') {
      S.m.integrations = keep(seq, await api().Integrations(''), S.m.integrations);
    } else if (sc === 'persona') {
      S.m.settings = keep(seq, await api().Settings(''), S.m.settings);
      seedForms(S.m.settings);
    } else if (sc === 'doctor') {
      S.m.doctor = keep(seq, await api().Doctor(''), S.m.doctor);
    } else if (sc === 'windows') {
      S.m.settings = keep(seq, await api().Settings(''), S.m.settings);
    } else if (sc === 'backups') {
      S.m.snaps = keep(seq, await api().Snapshots(''), S.m.snaps);
    } else if (sc === 'uninstall') {
      S.m.integrations = keep(seq, await api().Integrations(''), S.m.integrations);
      S.m.settings = keep(seq, await api().Settings(''), S.m.settings);
      seedForms(S.m.settings);
    } else if (sc === 'projects') {
      S.m.projects = keep(seq, await api().Projects(), S.m.projects);
    } else if (sc === 'project') {
      await loadProject(r, seq);
    }
  } catch (e) {
    if (seq !== S.m.seq) return;
    S.m.loadErr = t('gui.error.generic', { detail: errText(e) });
  }
  if (seq !== S.m.seq) return; // a newer load (another screen or project) owns the page now
  S.m.busy = false;
  render();
}

async function loadCredsQuiet() {
  try { S.cred.list = await api().Credentials(); S.cred.err = ''; await loadServersQuiet(); } catch (e) { S.cred.err = t('gui.delivery.cred.unavailable'); }
}

async function loadProject(root, seq) {
  S.m.projects = keep(seq, await soft(function () { return api().Projects(); }, S.m.projects || []), S.m.projects);
  S.m.pd = keep(seq, await api().Project(root), S.m.pd);
  if (seq !== S.m.seq) return;
  var tab = S.m.tab;
  if (tab === 'delivery') {
    seedDelivery(S.m.f.pdel, S.m.pd.delivery);
    await loadCredsQuiet();
  } else if (tab === 'index') {
    S.m.cg = keep(seq, await soft(function () { return api().CodeGraph(); }, null), S.m.cg);
  } else if (tab === 'memory' && S.m.pd.hasGabitotl) {
    S.m.settings = keep(seq, await api().Settings(root), S.m.settings);
    S.m.eng = keep(seq, await soft(function () { return api().Engram(); }, null), S.m.eng);
    seedForms(S.m.settings);
  } else if (tab === 'doctor' && S.m.pd.hasGabitotl) {
    S.m.doctor = keep(seq, await api().Doctor(root), S.m.doctor);
  } else if (tab === 'integrations' && S.m.pd.hasGabitotl) {
    S.m.integrations = keep(seq, await api().Integrations(root), S.m.integrations);
  } else if (tab === 'backups' && S.m.pd.hasGabitotl) {
    S.m.snaps = keep(seq, await api().Snapshots(root), S.m.snaps);
  }
}

function seedDelivery(dst, d) {
  if (!d) return;
  dst.automation = d.automation === 'custom' || !d.automation ? 'until-pr' : d.automation;
  dst.mergeMethod = d.mergeMethod || 'squash';
  dst.deleteBranch = !!d.deleteBranch;
  dst.draft = !!d.draft;
}

function seedGlobalDelivery(g) { seedDelivery(S.m.f.gdel, g); }

// seedForms copies the saved settings into the editable forms once per load.
function seedForms(st) {
  var f = S.m.f;
  f.lang = st.language || S.lang;
  f.mode = st.personaMode || 'normal';
  var mm = st.memory;
  if (mm && mm.configured) {
    f.mem.mode = mm.mode === 'team' ? 'team' : 'solo';
    f.mem.role = mm.role === 'client' ? 'client' : 'host';
    f.mem.url = mm.url || '';
    f.mem.tokenEnv = mm.tokenEnv || '';
    f.mem.instance = mm.instance || '';
  }
  (S.m.integrations || []).forEach(function (it) {
    if (S.m.un.runtimes[it.name] === undefined) S.m.un.runtimes[it.name] = true;
  });
}

function goTo(screen) {
  S.m.screen = screen;
  S.error = '';
  loadScreen();
}

actions.go = function (el) { goTo(el.getAttribute('data-val')); };

// openProject makes a folder the open project and remembers it.
async function openProject(path, tab) {
  S.m.proj = path;
  S.m.tab = tab || 'overview';
  S.m.screen = 'project';
  S.m.pd = null;
  S.m.focusTitle = true;
  S.root = path;
  S.error = '';
  try { await api().SetRoot(path); await api().AddProject(path); } catch (e) { /* remembering is a convenience */ }
  await loadScreen();
}

actions.popen = function (el) { openProject(el.getAttribute('data-path')); };

actions.tab = function (el) {
  S.m.tab = el.getAttribute('data-val');
  loadScreen();
};

// Arrow keys move between the tabs (roving tabindex).
document.addEventListener('keydown', function (ev) {
  var cur = ev.target && ev.target.getAttribute && ev.target.getAttribute('role') === 'tab' ? ev.target : null;
  if (!cur || S.m.modal) return;
  var tabs = Array.prototype.slice.call(document.querySelectorAll('[role="tab"]'));
  var i = tabs.indexOf(cur), n = -1;
  if (ev.key === 'ArrowRight') n = (i + 1) % tabs.length;
  else if (ev.key === 'ArrowLeft') n = (i - 1 + tabs.length) % tabs.length;
  else if (ev.key === 'Home') n = 0;
  else if (ev.key === 'End') n = tabs.length - 1;
  if (n < 0) return;
  ev.preventDefault();
  tabs[n].focus();
  actions.tab(tabs[n]);
});

actions.padd = async function () {
  try {
    var dir = await api().ChooseFolder(t('gui.detect.project'), S.root);
    if (!dir) return;
    await api().AddProject(dir);
    await openProject(dir);
  } catch (e) {
    toast(t('gui.error.generic', { detail: errText(e) }));
  }
};

actions.pforget = function (el) {
  var path = el.getAttribute('data-path');
  planAndRun({
    title: t('gui.projects.forget.title'), confirmLabel: t('gui.projects.forget'),
    plan: summaryPlan(t('gui.projects.forget.title'), [t('gui.projects.forget.plan', { path: path })]),
    run: async function () { await api().ForgetProject(path); return { message: t('gui.modal.done') }; },
    after: function () { if (S.m.proj === path) S.m.proj = ''; goTo('projects'); },
  });
};

// The project filter re-renders on every keystroke and keeps the caret.
document.addEventListener('input', function (ev) {
  var el = ev.target;
  if (!el || !el.hasAttribute || !el.hasAttribute('data-live') || ev.isComposing) return;
  set(el.getAttribute('data-bind'), el.value);
  var pos = el.selectionStart;
  render();
  var n = document.querySelector('[data-k="' + el.getAttribute('data-k') + '"]');
  if (n && n.setSelectionRange) { try { n.setSelectionRange(pos, pos); } catch (e) { /* not a text field */ } }
});

function toast(msg) {
  S.m.toast = msg;
  render();
  setTimeout(function () { if (S.m.toast === msg) { S.m.toast = ''; render(); } }, 2500);
}

// ---- modal --------------------------------------------------------------------

function openModal(m) {
  var a = document.activeElement;
  m.opener = (S.m.modal && S.m.modal.opener) || (a && a.getAttribute && a.getAttribute('data-k')) || '';
  S.m.modal = m;
  render();
}

function closeModal() {
  var opener = S.m.modal && S.m.modal.opener;
  S.m.modal = null;
  render();
  if (opener) {
    var el = document.querySelector('[data-k="' + opener.replace(/"/g, '\\"') + '"]');
    if (el) el.focus();
  }
}

actions.mact = function (el) {
  var m = S.m.modal;
  if (!m) return;
  var a = m.actions[parseInt(el.getAttribute('data-i'), 10)];
  if (a && a.fn && (!m.busy || a.always)) a.fn();
};

function modalHTML() {
  var m = S.m.modal;
  if (!m) return '';
  var acts = (m.actions || []).map(function (a, i) {
    return '<button type="button" class="btn' + (a.primary ? ' pri' : '') + (a.danger ? ' dng fill' : '') + '" data-act="mact" data-i="' + i + '" data-k="modal-' + i + '"' + attr('disabled', true, !!m.busy && !a.always) + '>' + esc(a.label) + '</button>';
  }).join('');
  return '<div class="back"><div class="modal" role="dialog" aria-modal="true" aria-labelledby="mtitle">' +
    '<h2 id="mtitle">' + esc(m.title) + '</h2><div class="mbody" tabindex="-1" id="mbody">' + m.body + '</div>' +
    '<div class="macts">' + acts + '</div></div></div>';
}

// afterRender keeps focus inside the dialog and the rest of the page inert.
function managerAfterRender() {
  var main = document.getElementById('mgr');
  if (!main) return;
  if (S.m.modal) {
    main.setAttribute('inert', '');
    var modal = document.querySelector('.modal');
    if (modal && !modal.contains(document.activeElement)) {
      var first = modal.querySelector('button:not([disabled]), input, select') || document.getElementById('mbody');
      if (first) first.focus();
    }
  } else {
    main.removeAttribute('inert');
    if (S.m.focusTitle && !S.m.busy) {
      S.m.focusTitle = false;
      var h = document.getElementById('ptitle');
      if (h) h.focus();
    }
  }
}

document.addEventListener('keydown', function (ev) {
  var m = S.m && S.m.modal;
  if (!m) return;
  if (ev.key === 'Escape') {
    if (!m.busy) { ev.preventDefault(); closeModal(); }
    return;
  }
  if (ev.key !== 'Tab') return;
  var nodes = Array.prototype.slice.call(document.querySelectorAll('.modal button:not([disabled]), .modal input, .modal select, .modal [tabindex="0"]'));
  if (!nodes.length) { ev.preventDefault(); return; }
  var i = nodes.indexOf(document.activeElement);
  var next = ev.shiftKey ? (i <= 0 ? nodes.length - 1 : i - 1) : (i === nodes.length - 1 ? 0 : i + 1);
  ev.preventDefault();
  nodes[next].focus();
});

function planHTML(p) {
  var html = '';
  if (p.supported === false) return '<p>' + esc(t('gui.modal.unsupported')) + '</p>';
  (p.groups || []).forEach(function (g) {
    html += '<h3>' + esc(g.label) + '</h3><ul>' + (g.lines || []).map(function (l) { return '<li>' + esc(l) + '</li>'; }).join('') + '</ul>';
  });
  if ((p.steps || []).length) html += '<h3>' + esc(t('gui.install.steps')) + '</h3><ul>' + p.steps.filter(function (s) { return s.title; }).map(function (s) { return '<li>' + esc(s.title) + '</li>'; }).join('') + '</ul>';
  (p.notes || []).forEach(function (n) { html += '<p class="hint">' + esc(n.text) + '</p>'; });
  (p.keep || []).forEach(function (n) { html += '<p class="hint">' + esc(n) + '</p>'; });
  if (!html) html = '<p>' + esc(t('gui.modal.nochange')) + '</p>';
  return html;
}

function runningHTML() {
  var m = S.m.modal;
  return '<div style="display:flex;font-size:13px"><b style="flex:1">' + esc(m.msg || t('gui.modal.running')) + '</b><span class="mono">' + m.pct + '%</span></div>' +
    '<div class="bar" role="progressbar" aria-label="' + esc(t('gui.install.progress')) + '" aria-valuenow="' + m.pct + '" aria-valuemin="0" aria-valuemax="100"><div style="width:' + m.pct + '%"></div></div>' +
    '<div class="log mono" role="log" aria-label="' + esc(t('gui.install.log')) + '">' + (m.log || []).map(logLine).join('') + '</div>';
}

function onOpProgress(ev) {
  var m = S.m && S.m.modal;
  if (!m || !m.busy || !ev) return;
  m.pct = Math.max(m.pct || 0, ev.percent || 0);
  if (ev.message) m.msg = ev.message;
  m.log.push(ev);
  m.body = runningHTML();
  render();
}

// listHTML renders a titled list of plain strings.
function listHTML(title, items) {
  if (!items || !items.length) return '';
  return '<h3>' + esc(title) + '</h3><ul>' + items.map(function (i) { return '<li class="mono">' + esc(i) + '</li>'; }).join('') + '</ul>';
}

// resultHTML shows what an operation did, whatever its result type: files
// written or removed, manual steps, the backup, failed steps and checks, and a
// rollback notice.
function resultHTML(r) {
  if (!r) return '<p>' + esc(t('gui.modal.done')) + '</p>';
  var html = '';
  if (r.rolledBack) html += '<p class="alert" style="text-align:left" role="alert">' + esc(t('gui.install.rolledback')) + '</p>';
  if (r.error) html += '<p class="alert" style="text-align:left" role="alert">' + esc(r.error) + '</p>';
  if (r.message) html += '<p>' + esc(r.message) + '</p>';
  if (r.signIn) html += '<p>' + esc(r.signIn.ok ? t('gui.delivery.signin.ok') : t('gui.delivery.signin.no')) + (r.signIn.detail ? ' · ' + esc(r.signIn.detail) : '') + '</p>';
  if (r.guard) {
    html += '<p>' + esc(t('gui.guard.scan.result', { count: r.guard.findings + (r.guard.history || []).length })) + '</p>';
    html += listHTML(t('gui.guard.scan.findings'), (r.guard.history || []).map(function (h) { return h.commit + ' ' + h.where; }));
  }
  if (r.runtime) html += resultHTML(r.runtime);
  [['applied', 'gui.modal.applied'], ['removed', 'gui.modal.removed'], ['changed', 'gui.modal.changed'], ['files', 'gui.modal.files']].forEach(function (p) {
    html += listHTML(t(p[1]), r[p[0]]);
  });
  html += listHTML(t('gui.modal.manual'), r.manual);
  var badSteps = (r.steps || []).filter(function (s) { return s.status === 'error'; }).map(function (s) { return s.id + ': ' + s.message; });
  html += listHTML(t('gui.install.step.warn'), badSteps);
  var badChecks = (r.checks || []).filter(function (c) { return !c.ok; }).map(function (c) { return c.group + ' ' + c.name + (c.detail ? ': ' + c.detail : ''); });
  html += listHTML(t('gui.modal.checks'), badChecks);
  (r.notes || r.notices || []).forEach(function (n) { html += '<p class="hint">' + esc(n.text) + '</p>'; });
  if (r.backup) html += '<p class="hint">' + esc(t('gui.modal.backup', { path: r.backup })) + '</p>';
  return html || '<p>' + esc(t('gui.modal.done')) + '</p>';
}

// planAndRun shows the plan, waits for the person to confirm, runs it with a
// progress log and shows the result. o: title, plan(), run(planId), after(),
// confirmLabel, danger, extra (html), check() (returns an error text or ''),
// cancel() (stops a running operation; shows a Cancel button while it runs).
async function planAndRun(o) {
  openModal({ title: o.title, body: '<p>' + esc(t('gui.modal.planning')) + '</p>', actions: [], busy: true, pct: 0, log: [] });
  var plan;
  try {
    plan = await o.plan();
  } catch (e) {
    return showEnd(o, '<p class="alert" style="text-align:left" role="alert">' + esc(errText(e)) + '</p>');
  }
  if (plan.supported === false) return showEnd(o, planHTML(plan));
  if (!plan.mutates && !o.confirmAlways) return execute(plan);
  var m = S.m.modal;
  m.busy = false;
  m.body = extraHTML(o, plan) + '<div id="plan">' + planHTML(plan) + '</div>';
  m.actions = [
    { label: t('gui.btn.cancel'), fn: closeModal },
    { label: o.confirmLabel || t('gui.btn.apply'), primary: !o.danger, danger: !!o.danger, fn: function () { execute(plan); } },
  ];
  render();

  async function execute(pl) {
    if (o.check) {
      var bad = o.check(pl);
      if (bad) {
        S.m.modal.body = '<p class="alert" style="text-align:left" role="alert">' + esc(bad) + '</p>' + extraHTML(o, pl) + planHTML(pl);
        return render();
      }
    }
    var mm = S.m.modal;
    mm.busy = true; mm.pct = 0; mm.log = []; mm.msg = '';
    mm.body = runningHTML();
    mm.actions = o.cancel ? [{ label: t('gui.btn.cancel'), always: true, fn: o.cancel }] : [];
    render();
    var html;
    try {
      var res = await o.run(pl.id, pl);
      html = resultHTML(res);
      if (o.onResult) o.onResult(res);
    } catch (e) {
      html = '<p class="alert" style="text-align:left" role="alert">' + esc(errText(e)) + '</p>';
    }
    showEnd(o, html);
  }
}

function extraHTML(o, plan) {
  return typeof o.extra === 'function' ? o.extra(plan) : (o.extra || '');
}

function showEnd(o, html) {
  var m = S.m.modal;
  m.busy = false;
  m.body = html;
  m.actions = [{ label: t('gui.btn.close'), primary: true, fn: function () {
    closeModal();
    if (o.quitAfter && o.quitAfter()) { api().Quit(); return; }
    if (o.after) o.after(); else loadScreen();
  } }];
  render();
}

// summaryPlan is a plan made of plain lines, for settings that are written
// directly (no backend plan): the person still confirms before anything is saved.
function summaryPlan(label, lines) {
  return async function () { return { id: '', mutates: true, supported: true, groups: [{ label: label, lines: lines }], steps: [], notes: [] }; };
}

// ---- shell --------------------------------------------------------------------

function navBtn(id, key, current, ic) {
  return '<button type="button" class="nav" data-act="go" data-val="' + id + '" data-k="nav-' + id + '"' + (current ? ' aria-current="page"' : '') + '>' + UI.icon(ic) + '<span>' + esc(t(key)) + '</span></button>';
}

function renderManager() {
  var sc = S.m.screen;
  var nav = '<div class="grp">' + esc(t('gui.mgr.nav.global')) + '</div>' + GLOBAL_NAV.map(function (n) { return navBtn(n[0], n[1], sc === n[0], n[2]); }).join('') +
    '<div class="grp">' + esc(t('gui.mgr.nav.projects-group')) + '</div>' + navBtn('projects', 'gui.mgr.nav.projects', sc === 'projects', 'folders');
  if (S.m.proj) {
    nav += '<button type="button" class="nav" data-act="popen" data-path="' + esc(S.m.proj) + '" data-k="nav-current" title="' + esc(S.m.proj) + '"' + (sc === 'project' ? ' aria-current="page"' : '') + '>' + UI.icon('folder') + '<span>' + esc(t('gui.mgr.nav.current')) + ': ' + esc(projectName()) + '</span></button>';
  }
  var side = '<nav class="side" aria-label="' + esc(t('gui.mgr.nav.label')) + '">' + nav +
    '<div class="foot mono"><span>v' + esc(S.info.version) + '</span></div></nav>';
  var body = S.m.loadErr ? '<p class="lead" role="alert">' + esc(S.m.loadErr) + '</p>' : screenHTML();
  return '<div id="mgr" style="display:flex;flex-direction:column;flex:1;min-height:0">' + titlebar('gui.mgr.title') + '<div class="body">' + side +
    '<main class="pane" id="pane">' + body + '</main></div></div>' + modalHTML() +
    (S.m.toast ? '<div class="toast" role="status">' + esc(S.m.toast) + '</div>' : '');
}

function screenHTML() {
  switch (S.m.screen) {
    case 'overview': return screenOverview();
    case 'delivery': return screenGlobalDelivery();
    case 'codegraph': return screenCodeGraph();
    case 'integrations': return screenIntegrations();
    case 'persona': return screenPersona();
    case 'doctor': return screenDoctor();
    case 'windows': return screenWindows();
    case 'backups': return screenBackups();
    case 'uninstall': return screenUninstall();
    case 'projects': return screenProjects();
    case 'project': return screenProject();
  }
  return '';
}

var TITLES = {
  overview: ['gui.mgr.nav.overview', 'gui.ov.global.lead', 'home'], doctor: ['gui.doc.title', 'gui.doc.lead', 'pulse'],
  integrations: ['gui.mgr.nav.integrations', 'gui.int.lead', 'plug'], persona: ['gui.mgr.nav.persona', 'gui.persona.lead', 'chat'],
  memory: ['gui.mgr.nav.memory', 'gui.memory.lead', 'db'], delivery: ['gui.glob.delivery.title', 'gui.glob.delivery.lead', 'pr'],
  codegraph: ['gui.cg.title', 'gui.cg.lead', 'graph'], windows: ['gui.ov.windows', 'gui.windows.lead', 'desktop'],
  guard: ['gui.mgr.nav.guard', 'gui.guard.lead', 'shield'], backups: ['gui.mgr.nav.backups', 'gui.bk.lead', 'archive'],
  uninstall: ['gui.un.title', 'gui.un.lead', 'trash'], projects: ['gui.projects.title', 'gui.projects.lead', 'folders'],
};

function head(sc, buttons) {
  var tl = TITLES[sc];
  if (S.m.screen === 'project') return '<div class="hrow"><div class="grow"><p class="lead">' + esc(t(tl[1])) + '</p></div>' + (buttons || '') + '</div>';
  return '<div class="hrow"><div class="grow">' + phead(tl[2], t(tl[0]), t(tl[1]), { id: 'ptitle' }) + '</div>' + (buttons || '') + '</div>';
}

// While a screen loads a shimmering skeleton stands in for it.
function busyLine() { return S.m.busy ? UI.skeleton() : ''; }

function link(act, val, label, k) {
  return '<button type="button" class="btn xs" data-act="' + act + '" data-val="' + esc(val) + '" data-k="' + k + '">' + esc(label) + '</button>';
}

// ---- global overview ------------------------------------------------------------------

function counts(rep) {
  var c = { ok: 0, warn: 0, todo: 0, error: 0 };
  ((rep && rep.sections) || []).forEach(function (s) { s.lines.forEach(function (l) { if (c[l.status] !== undefined) c[l.status]++; }); });
  return c;
}

var STAT_ICON = { 'gui.doc.ok': ['ok', 'check'], 'gui.doc.warn': ['warn', 'warn'], 'gui.doc.todo': ['todo', 'info'], 'gui.doc.error': ['err', 'error'] };

function stat(n, key) {
  var s = STAT_ICON[key] || ['todo', 'info'];
  return '<div class="stat s-' + s[0] + '">' + UI.icon(s[1]) + '<b>' + esc(n) + '</b>' + esc(t(key)) + '</div>';
}

function cgSummary() {
  var g = S.m.cg;
  if (!g) return t('gui.windows.codegraph.checking');
  return codegraphLines().text;
}

function deliverySummary() {
  var g = S.m.gd;
  if (!g) return '-';
  var key = { all: 'gui.delivery.auto.all', 'until-pr': 'gui.delivery.auto.until-pr', ask: 'gui.delivery.auto.ask' }[g.automation];
  return (key ? t(key) : g.automation) + (g.draft ? ' · ' + t('gui.delivery.draft') : '');
}

function screenOverview() {
  var c = counts(S.m.doctor);
  var ints = S.m.integrations || [];
  var managed = ints.filter(function (i) { return i.managed; }).length;
  var stored = (S.cred.list || []).filter(function (x) { return x.stored; }).length;
  var recent = (S.m.projects || []).slice(0, 5);
  var out = head('overview') + busyLine() +
    '<div class="stats">' + stat(c.ok, 'gui.doc.ok') + stat(c.warn, 'gui.doc.warn') + stat(c.todo, 'gui.doc.todo') + stat(c.error, 'gui.doc.error') + '</div>' +
    '<dl class="sum"><dt>' + esc(t('gui.ov.version')) + '</dt><dd>' + esc(S.info.version) + '</dd>' +
    '<dt>' + esc(t('gui.mgr.nav.integrations')) + '</dt><dd>' + esc(t('gui.ov.connected', { n: managed, total: ints.length })) + '</dd>' +
    '<dt>' + esc(t('gui.ov.codegraph')) + '</dt><dd>' + esc(cgSummary()) + '</dd>' +
    '<dt>' + esc(t('gui.ov.delivery')) + '</dt><dd>' + esc(deliverySummary()) + '</dd>' +
    '<dt>' + esc(t('gui.ov.creds')) + '</dt><dd>' + esc(t('gui.ov.creds.n', { n: stored, total: (S.cred.list || []).length })) + '</dd></dl>' +
    '<div class="lbl">' + esc(t('gui.ov.actions')) + '</div><div class="acts">' +
    '<button type="button" class="btn" data-act="go" data-val="doctor" data-k="qa-doctor">' + esc(t('gui.ov.run-doctor')) + '</button>' +
    '<button type="button" class="btn" data-act="go" data-val="integrations" data-k="qa-int">' + esc(t('gui.ov.connect')) + '</button>' +
    '<button type="button" class="btn" data-act="go" data-val="backups" data-k="qa-bk">' + esc(t('gui.ov.backups')) + '</button></div>' +
    '<div class="lbl">' + esc(t('gui.ov.projects')) + '</div><p class="hint">' + esc(t('gui.projects.lead')) + '</p>';
  if (S.root && S.m.proj !== S.root) {
    out += '<div><button type="button" class="btn pri" data-act="popen" data-path="' + esc(S.root) + '" data-k="continue">' + esc(t('gui.ov.projects.continue', { name: baseName(S.root) })) + '</button></div>';
  }
  if (!recent.length && !S.m.busy) out += UI.emptyState('folders', t('gui.ov.projects.none'), addProjectBtn('ov-padd'));
  else out += projectRows(recent, 'ov');
  out += '<div class="acts"><button type="button" class="btn" data-act="go" data-val="projects" data-k="qa-projects">' + esc(t('gui.ov.projects.all')) + '</button></div>';
  return out;
}

// addProjectBtn is the call to action of the empty project lists.
function addProjectBtn(k) {
  return '<button type="button" class="btn pri" data-act="padd" data-k="' + k + '">' + UI.icon('plus') + esc(t('gui.projects.add')) + '</button>';
}

function desktopCard() {
  var d = S.m.settings && S.m.settings.desktop;
  if (!d || !d.supported) return '<p class="lead">' + esc(t('gui.windows.unsupported')) + '</p>';
  var rows = [['startMenu', 'gui.windows.startmenu', d.startMenu, 'menu', 'startmenu'], ['desktopShortcut', 'gui.windows.desktop', d.desktopShortcut, 'desktop', 'desktop'], ['addToPath', 'gui.windows.path', d.onPath, 'terminal', 'path']];
  return '<div class="box"><div class="row"><span class="oic sm">' + UI.icon('folder') + '</span><b>' + esc(t('gui.windows.dir')) + '</b><span class="mono">' + esc(d.installDir) + '</span></div>' +
    rows.map(function (r) {
      return '<div class="row"' + UI.popAttr(r[4]) + ' data-pk="desk-' + r[0] + '"><span class="oic sm">' + UI.icon(r[3]) + '</span><b>' + esc(t(r[1])) + '</b>' + (r[2] ? badge('ok', t('gui.ov.in-place')) : badge('todo', t('gui.ov.missing'))) + '<span class="sp"></span>' +
        (r[2] ? '' : '<button type="button" class="btn xs" data-act="desk" data-val="' + r[0] + '" data-k="desk-' + r[0] + '">' + UI.icon('plus') + esc(t('gui.ov.add')) + '</button>') + UI.infoBtn(r[4]).replace('class="pinfo"', 'class="pinfo" style="position:static"') + '</div>';
    }).join('') + '</div>';
}

function screenWindows() { return head('windows') + busyLine() + desktopCard(); }

actions.desk = function (el) {
  var d = S.m.settings.desktop;
  var w = { installDir: d.installDir, startMenu: d.startMenu, desktopShortcut: d.desktopShortcut, addToPath: d.onPath, guardHooks: false, codeIndex: '' };
  w[el.getAttribute('data-val')] = true;
  planAndRun({ title: t('gui.ov.windows'), plan: function () { return api().PlanDesktop(w); }, run: function (id) { return api().ApplyDesktop(w, id); } });
};

// ---- global delivery defaults and credentials ---------------------------------------------

function screenGlobalDelivery() {
  return head('delivery') + busyLine() + deliveryForm('m.f.gdel') +
    '<div><button type="button" class="btn pri" data-act="gdsave" data-k="gd-save">' + esc(t('gui.btn.save')) + '</button></div>' +
    credentialsBlock();
}

function deliveryAnswers(d) {
  var all = d.automation === 'all';
  return { automation: d.automation, mergeMethod: all ? d.mergeMethod : '', deleteBranch: all ? !!d.deleteBranch : null, draft: !!d.draft };
}

function deliveryLines(d) {
  return [t('gui.delivery.automation') + ': ' + d.automation, t('gui.delivery.draft') + ': ' + !!d.draft];
}

actions.gdsave = function () {
  var d = S.m.f.gdel;
  planAndRun({
    title: t('gui.glob.delivery.title'), confirmLabel: t('gui.btn.save'),
    plan: summaryPlan(t('gui.glob.delivery.title'), deliveryLines(d)),
    run: async function () { await api().SetGlobalDelivery(deliveryAnswers(d)); return { message: t('gui.modal.done') }; },
  });
};

// ---- CodeGraph ----------------------------------------------------------------------------

function screenCodeGraph() {
  var g = S.m.cg;
  var out = head('codegraph') + busyLine();
  if (!g) return out;
  var cg = codegraphLines();
  var fix = g.supported && g.needsWork;
  var actKey = !g.installed ? 'gui.cg.act.install' : (g.action === 'upgrade' ? 'gui.cg.act.upgrade' : 'gui.cg.act.repair');
  out += '<div class="box"><div class="row"><span class="oic sm">' + UI.icon('graph') + '</span><b>' + esc(t('gui.cg.version')) + '</b>' + (g.installed ? '<span class="mono">' + esc(g.version) + '</span>' : badge('warn', t('gui.cg.missing'))) + '</div>' +
    '<div class="row"><b>' + esc(t('gui.cg.pinned')) + '</b><span class="mono">' + esc(g.pinned) + '</span></div>' +
    (g.path ? '<div class="row"><b>' + esc(t('gui.cg.path')) + '</b><span class="mono">' + esc(g.path) + '</span></div>' : '') + '</div>' +
    '<div class="note" role="status">' + UI.icon('info') + '<span>' + esc(cg.text) + (cg.shadow ? ' ' + esc(cg.shadow) : '') + '</span></div>';
  out += '<div class="lbl">' + esc(t('gui.cg.agents')) + '</div>';
  if (!(g.agents || []).length) out += '<p class="hint">' + esc(t('gui.cg.agents.none')) + '</p>';
  else {
    out += '<div class="box">' + g.agents.map(function (a) {
      return '<div class="row"><span class="oic sm">' + UI.icon(rtIcon(a.runtime)) + '</span><b>' + esc(a.label) + '</b>' + (a.registered ? badge('ok', t('gui.cg.registered')) : badge('warn', t('gui.cg.unregistered'))) + '</div>';
    }).join('') + '</div>';
  }
  if (fix) out += '<div><button type="button" class="btn pri" data-act="cgfix" data-k="cg-fix"' + UI.popAttr('cg-fix') + '>' + UI.icon('wrench') + esc(t(actKey)) + '</button></div>';
  else if (g.supported) out += '<p class="hint">' + esc(t('gui.cg.uptodate')) + '</p>';
  return out;
}

actions.cgfix = function () {
  planAndRun({
    title: t('gui.cg.title'), plan: function () { return api().PlanCodeGraph(); },
    run: async function (id) { var steps = await api().ApplyCodeGraph(id); return { steps: steps }; },
  });
};

// ---- projects -----------------------------------------------------------------------------

function srcBadges(p) {
  return (p.sources || []).map(function (s) { return badge('todo', SRC_KEYS[s] ? t(SRC_KEYS[s]) : s, SRC_ICON[s] || 'folder'); }).join(' ');
}

// platformBadge is the platform of a project's remote as a glyph and its host.
function platformBadge(pd) {
  if (!pd.platform) return badge('warn', t('gui.proj.platform.none'), 'ban');
  return badge('todo', pd.host ? pd.platform + ' · ' + pd.host : pd.platform, CRED_ICON[pd.platform] || 'cloud');
}

function fmtDate(iso) {
  if (!iso) return '';
  var d = new Date(iso);
  return isNaN(d.getTime()) ? '' : d.toLocaleDateString(S.lang === 'en' ? 'en-US' : 'es-AR');
}

// projectRows renders the list; the person opens, and optionally forgets, a row.
function projectRows(list, id) {
  return '<ul class="plist" role="list" aria-label="' + esc(t('gui.projects.list')) + '">' + list.map(function (p, i) {
    var seen = fmtDate(p.lastSeen);
    return '<li class="prow"><span class="oic sm" aria-hidden="true">' + UI.icon(p.isGit ? 'branch' : 'folder') + '</span><div class="pmain"><b>' + esc(p.name) + '</b><span class="mono" title="' + esc(p.path) + '">' + esc(p.path) + '</span>' +
      '<span class="pbadges">' + srcBadges(p) + (p.hasGabitotl ? '' : ' ' + badge('warn', t('gui.projects.notset'))) + (seen ? ' <span class="hint">' + esc(t('gui.projects.lastseen', { date: seen })) + '</span>' : '') + '</span></div>' +
      '<div class="acts"><button type="button" class="btn xs pri" data-act="popen" data-path="' + esc(p.path) + '" data-k="' + id + '-open-' + i + '" aria-label="' + esc(t('gui.projects.open') + ': ' + p.name) + '">' + esc(t('gui.projects.open')) + '</button>' +
      (id === 'pl' ? '<button type="button" class="btn xs dng" data-act="pforget"' + UI.popAttr('forget') + ' data-path="' + esc(p.path) + '" data-k="pl-forget-' + i + '" aria-label="' + esc(t('gui.projects.forget') + ': ' + p.name) + '">' + esc(t('gui.projects.forget')) + '</button>' : '') + '</div></li>';
  }).join('') + '</ul>';
}

function filteredProjects() {
  var q = S.m.pf.trim().toLowerCase();
  return (S.m.projects || []).filter(function (p) {
    if (S.m.psrc !== 'all' && (p.sources || []).indexOf(S.m.psrc) < 0) return false;
    return !q || p.name.toLowerCase().indexOf(q) >= 0 || p.path.toLowerCase().indexOf(q) >= 0;
  });
}

function screenProjects() {
  var btns = '<button type="button" class="btn" data-act="rerun" data-k="prefresh"' + attr('disabled', true, S.m.busy) + '>' + UI.icon('refresh') + esc(t('gui.projects.refresh')) + '</button>' +
    '<button type="button" class="btn pri" data-act="padd" data-k="padd">' + UI.icon('plus') + esc(t('gui.projects.add')) + '</button>';
  var out = head('projects', btns) + busyLine();
  var list = filteredProjects();
  out += '<div class="hrow"><label class="field grow">' + esc(t('gui.projects.search')) +
    '<input type="search" data-bind="m.pf" data-live data-k="m.pf" value="' + esc(S.m.pf) + '"></label>' +
    '<label class="field">' + esc(t('gui.projects.source')) + '<select data-bind="m.psrc" data-k="m.psrc">' +
    '<option value="all"' + attr('selected', true, S.m.psrc === 'all') + '>' + esc(t('gui.projects.source.all')) + '</option>' +
    Object.keys(SRC_KEYS).map(function (k) { return '<option value="' + k + '"' + attr('selected', true, S.m.psrc === k) + '>' + esc(t(SRC_KEYS[k])) + '</option>'; }).join('') +
    '</select></label></div>';
  if (!list.length) return out + (S.m.busy ? '' : UI.emptyState('search', t(S.m.pf || S.m.psrc !== 'all' ? 'gui.projects.empty' : 'gui.ov.projects.none'), addProjectBtn('pl-padd')));
  return out + '<p class="hint" role="status">' + esc(t('gui.projects.count', { n: list.length })) + '</p>' + projectRows(list, 'pl');
}

// ---- project page -----------------------------------------------------------------------

function projectEntry() {
  return (S.m.projects || []).filter(function (p) { return p.path === S.m.proj; })[0] || null;
}

function screenProject() {
  var pd = S.m.pd;
  var out = '<div class="hrow"><div class="grow"><button type="button" class="btn xs" data-act="go" data-val="projects" data-k="pback">' + UI.icon('arrow-left') + esc(t('gui.proj.back')) + '</button>' +
    '<h1 id="ptitle" tabindex="-1">' + esc(pd ? pd.name : projectName()) + '</h1><p class="lead mono">' + esc(S.m.proj) + '</p>' +
    (pd ? '<div class="acts" style="margin-top:8px">' + platformBadge(pd) + (pd.hasGabitotl ? badge('ok', t('gui.proj.status.on')) : badge('warn', t('gui.projects.notset'))) + '</div>' : '') + '</div></div>';
  if (!pd) return out + busyLine();
  var tabs = '<div class="tabs" role="tablist" aria-label="' + esc(t('gui.proj.tabs')) + '">' + PROJECT_TABS.map(function (tb) {
    var on = S.m.tab === tb[0];
    return '<button type="button" role="tab" class="tab" id="tab-' + tb[0] + '" aria-selected="' + on + '" aria-controls="tabpanel" tabindex="' + (on ? 0 : -1) + '" data-act="tab" data-val="' + tb[0] + '" data-k="tab-' + tb[0] + '">' + UI.icon(tb[3]) + esc(t(tb[1])) + '</button>';
  }).join('') + '</div>';
  var cur = PROJECT_TABS.filter(function (tb) { return tb[0] === S.m.tab; })[0] || PROJECT_TABS[0];
  var panel = '';
  if (cur[2] && !pd.hasGabitotl) panel += needsSetup();
  else panel += projectTab(cur[0]);
  return out + tabs + '<div id="tabpanel" role="tabpanel" aria-labelledby="tab-' + cur[0] + '" tabindex="0" class="tabpanel">' + panel + '</div>';
}

function needsSetup() {
  return '<div class="note">' + UI.icon('info') + '<span>' + esc(t('gui.proj.needs-setup')) + '</span></div>' +
    '<div><button type="button" class="btn pri" data-act="psetup" data-k="psetup"' + UI.popAttr('project-setup') + '>' + UI.icon('plus') + esc(t('gui.proj.setup')) + '</button></div>';
}

function projectTab(tab) {
  switch (tab) {
    case 'overview': return projOverview();
    case 'delivery': return projDelivery();
    case 'index': return projIndex();
    case 'guard': return projGuard();
    case 'memory': return screenMemory();
    case 'doctor': return screenDoctor();
    case 'integrations': return screenIntegrations();
    case 'backups': return screenBackups();
  }
  return '';
}

function projOverview() {
  var pd = S.m.pd, e = projectEntry();
  var out = '<dl class="sum"><dt>' + esc(t('gui.proj.path')) + '</dt><dd class="mono">' + esc(pd.path) + '</dd>' +
    '<dt>' + esc(t('gui.proj.git')) + '</dt><dd>' + esc(pd.isGit ? t('gui.proj.yes') : t('gui.proj.no')) + '</dd>' +
    '<dt>' + esc(t('gui.proj.platform')) + '</dt><dd>' + platformBadge(pd) + '</dd>' +
    '<dt>' + esc(t('gui.proj.status')) + '</dt><dd>' + (pd.hasGabitotl ? badge('ok', t('gui.proj.status.on')) : badge('warn', t('gui.projects.notset'))) + '</dd>' +
    (e && e.sources && e.sources.length ? '<dt>' + esc(t('gui.proj.sources')) + '</dt><dd>' + srcBadges(e) + '</dd>' : '') +
    (e && e.lastSeen ? '<dt>' + esc(t('gui.proj.lastseen')) + '</dt><dd>' + esc(fmtDate(e.lastSeen)) + '</dd>' : '') + '</dl>';
  if (!pd.hasGabitotl) {
    out += '<div class="note">' + UI.icon('info') + '<span>' + esc(t('gui.proj.setup.lead')) + '</span></div>' +
      '<div><button type="button" class="btn pri" data-act="psetup" data-k="psetup"' + UI.popAttr('project-setup') + '>' + UI.icon('plus') + esc(t('gui.proj.setup')) + '</button></div>';
  }
  return out;
}

actions.psetup = function () {
  var root = S.m.proj;
  planAndRun({
    title: t('gui.proj.setup'), confirmLabel: t('gui.btn.apply'),
    plan: function () { return api().PlanProjectSetup(root); },
    run: function (id) { return api().ApplyProjectSetup(root, id); },
  });
};

function projDelivery() {
  var pd = S.m.pd, d = pd.delivery;
  var srcKey = { project: 'gui.proj.delivery.source.project', global: 'gui.proj.delivery.source.global', 'default': 'gui.proj.delivery.source.default' }[d.source] || 'gui.proj.delivery.source.default';
  var out = '<p class="lead">' + esc(t('gui.proj.delivery.lead')) + '</p>' +
    '<div class="note" role="status">' + UI.icon('info') + '<span><b>' + esc(t('gui.proj.delivery.source')) + ':</b> ' + esc(t(srcKey)) + '</span></div>' +
    deliveryForm('m.f.pdel') +
    '<div class="acts"><button type="button" class="btn pri" data-act="pdsave" data-k="pd-save">' + esc(t('gui.proj.delivery.save')) + '</button>' +
    '<button type="button" class="btn" data-act="pdreset" data-k="pd-reset"' + attr('disabled', true, d.source !== 'project') + '>' + esc(t('gui.proj.delivery.reset')) + '</button></div>';
  out += '<div class="lbl">' + esc(t('gui.delivery.creds.title')) + '</div>';
  var c = pd.credential;
  if (!c.known) return out + '<p class="hint">' + esc(t('gui.proj.cred.none')) + '</p>';
  var kind = c.server ? t('gui.proj.cred.server', { platform: srvPlatformLabel(c.platform), url: c.serverUrl || '' }) : (CRED_PLATFORMS.some(function (p) { return p[0] === c.platform; }) ? t('gui.proj.cred.public', { platform: srvPlatformLabel(c.platform) }) : '');
  out += '<div class="box"><div class="row"><b style="width:auto">' + esc(t('gui.proj.cred', { host: c.host })) + '</b><span class="sp"></span>' +
    (c.stored ? badge('ok', t('gui.proj.cred.stored')) : badge('todo', t('gui.proj.cred.missing'))) +
    (c.stored ? credBtn('credtest', t('gui.delivery.cred.test'), 'proj', 'data-key="proj" data-cred="' + esc(c.platform || '') + '" data-host="' + esc(c.host) + '" ') : '') + credMsgHTML('proj') + '</div>' +
    (kind ? '<div class="row"><span class="hint">' + esc(kind) + '</span></div>' : '') + '</div>';
  var pub = c.platform === 'github' || c.platform === 'gitlab' || c.platform === 'bitbucket';
  if (!c.server && !(pub && /^(github\.com|gitlab\.com|bitbucket\.org)$/.test(c.host))) out += '<p class="hint">' + esc(t('gui.proj.cred.unknown')) + '</p>';
  if (!c.stored) out += '<div>' + link('go', 'delivery', t('gui.proj.cred.link'), 'pd-cred-link') + '</div>';
  return out;
}

actions.pdsave = function () {
  var d = S.m.f.pdel, root = S.m.proj;
  var ans = deliveryAnswers(d);
  ans.provider = '';
  ans.tokenEnv = '';
  planAndRun({
    title: t('gui.mgr.nav.delivery'), confirmLabel: t('gui.btn.save'),
    plan: summaryPlan(t('gui.mgr.nav.delivery'), deliveryLines(d)),
    run: function () { return api().SetDelivery(root, ans); },
  });
};

actions.pdreset = function () {
  var root = S.m.proj;
  planAndRun({
    title: t('gui.proj.delivery.reset'), confirmLabel: t('gui.proj.delivery.reset'),
    plan: summaryPlan(t('gui.proj.delivery.reset'), [t('gui.proj.delivery.reset.plan')]),
    run: function () { return api().ResetProjectDelivery(root); },
  });
};

function projIndex() {
  var pd = S.m.pd;
  var cgOk = S.m.cg ? !!S.m.cg.installed : true;
  var out = '<p class="lead">' + esc(t('gui.proj.index.lead')) + '</p>';
  if (!cgOk) {
    out += '<div class="note" role="status">' + UI.icon('info') + '<span>' + esc(t('gui.proj.index.needs')) + '</span></div><div>' + link('go', 'codegraph', t('gui.proj.index.goto'), 'pi-goto') + '</div>';
  }
  if (!pd.index.length) return out + '<p class="hint">' + esc(t('gui.guard.index.none')) + '</p>';
  out += '<div class="box">';
  pd.index.forEach(function (b, i) {
    var state = b.indexed ? badge('ok', t('gui.guard.indexed')) : (b.installed ? badge('todo', t('gui.guard.not-indexed')) : badge('warn', t('gui.guard.not-installed')));
    out += '<div class="row"><span class="oic sm">' + UI.icon('graph') + '</span><b>' + esc(b.name) + '</b>' + state + (!b.installed && b.installHint ? '<span class="mono">' + esc(b.installHint) + '</span>' : '') + '<span class="sp"></span>' +
      '<button type="button" class="btn xs' + (b.indexed ? '' : ' pri') + '" data-act="pindex" data-i="' + i + '" data-k="pindex-' + i + '"' + (b.installed ? UI.popAttr('reindex') : '') + attr('disabled', true, !b.installed) + attr('title', t('gui.guard.not-installed'), !b.installed) + '>' + esc(b.indexed ? t('gui.proj.index.rerun') : t('gui.proj.index.run')) + '</button></div>';
  });
  return out + '</div>';
}

actions.pindex = function (el) {
  var root = S.m.proj;
  var b = S.m.pd.index[+el.getAttribute('data-i')];
  planAndRun({
    title: t('gui.proj.index.running', { name: b.name }), confirmLabel: b.indexed ? t('gui.proj.index.rerun') : t('gui.proj.index.run'),
    plan: summaryPlan(t('gui.windows.index'), [t('gui.proj.index.plan', { name: b.name, path: root })]),
    run: async function () { await api().IndexProject(root, b.name); return { message: t('gui.modal.done') }; },
    cancel: function () { api().CancelOperation(); },
  });
};

function projGuard() {
  var g = S.m.pd.guard;
  return '<p class="lead">' + esc(t('gui.guard.lead')) + '</p><div class="box"><div class="row"><span class="oic sm">' + UI.icon('shield') + '</span><b>' + esc(t('gui.guard.hooks')) + '</b>' +
    (g.hooksInstalled ? badge('ok', t('gui.guard.active')) : badge('todo', t('gui.guard.inactive'))) + '<span class="sp"></span>' +
    '<button type="button" class="btn xs" data-act="hooks"' + UI.popAttr('hooks') + ' data-val="' + (g.hooksInstalled ? 'remove' : 'install') + '" data-k="hooks">' + esc(g.hooksInstalled ? t('gui.guard.remove') : t('gui.guard.install')) + '</button></div>' +
    '<div class="row"><span class="oic sm">' + UI.icon('search') + '</span><b>' + esc(t('gui.guard.history')) + '</b><span class="sp"></span><button type="button" class="btn xs" data-act="scanhist"' + UI.popAttr('scan-history') + ' data-k="scanhist">' + esc(t('gui.guard.scan')) + '</button></div></div>';
}

// ---- doctor -------------------------------------------------------------------

var ICONS = { ok: ['ok', 'check', 'gui.doc.ok'], warn: ['warn', 'warn', 'gui.doc.warn'], todo: ['todo', 'info', 'gui.doc.todo'], error: ['err', 'error', 'gui.doc.error'] };

function screenDoctor() {
  var rep = S.m.doctor;
  var btns = '<button type="button" class="btn" data-act="openlog" data-k="openlog">' + UI.icon('folder') + esc(t('gui.doc.log')) + '</button>' +
    '<button type="button" class="btn" data-act="copy" data-k="copy"' + attr('disabled', true, !rep) + '>' + UI.icon('copy') + esc(t('gui.doc.copy')) + '</button>' +
    '<button type="button" class="btn pri" data-act="rerun" data-k="rerun"' + attr('disabled', true, S.m.busy) + '>' + UI.icon('refresh') + esc(t('gui.doc.rerun')) + '</button>';
  var out = head('doctor', btns) + busyLine();
  if (!rep) return out;
  var c = counts(rep);
  out += '<div class="stats">' + stat(c.ok, 'gui.doc.ok') + stat(c.warn, 'gui.doc.warn') + stat(c.todo, 'gui.doc.todo') + stat(c.error, 'gui.doc.error') + '</div><div class="list">';
  rep.sections.forEach(function (sec, si) {
    out += '<div class="sec">' + esc(sec.title) + '</div>';
    sec.lines.forEach(function (l, li) {
      var ic = ICONS[l.status] || ICONS.ok;
      out += '<div class="row"><span class="ic ' + ic[0] + '" role="img" aria-label="' + esc(t(ic[2])) + '">' + UI.icon(ic[1]) + '</span><span>' + esc(l.message) + '</span><span class="sp"></span>' +
        (l.detail && l.detail !== l.message ? '<span class="mono">' + esc(l.detail) + '</span>' : '') +
        (l.fix ? '<button type="button" class="btn xs" data-act="fix" data-s="' + si + '" data-l="' + li + '" data-k="fix-' + si + '-' + li + '">' + esc(l.fix.label) + '</button>' : '') + '</div>';
    });
  });
  return out + '</div>';
}

actions.openlog = async function () {
  try {
    await api().OpenLogFolder();
  } catch (e) {
    toast(t('gui.error.generic', { detail: errText(e) }));
  }
};

actions.rerun = function () { loadScreen(); };

actions.copy = async function () {
  var rep = S.m.doctor;
  if (!rep) return;
  var lines = [rep.title, ''];
  rep.sections.forEach(function (s) {
    lines.push('## ' + s.title);
    s.lines.forEach(function (l) { lines.push('[' + l.status + '] ' + l.message); });
    lines.push('');
  });
  lines.push(rep.summary);
  try {
    await api().CopyText(lines.join('\n'));
    toast(t('gui.doc.copied'));
  } catch (e) {
    toast(t('gui.error.generic', { detail: errText(e) }));
  }
};

actions.fix = function (el) {
  var l = S.m.doctor.sections[+el.getAttribute('data-s')].lines[+el.getAttribute('data-l')];
  var req = { root: PR(), id: l.fix.id, args: l.fix.args || {} };
  planAndRun({
    title: l.fix.label, plan: function () { return api().PlanFix(req); },
    run: function (id) { return api().RunFix(req, id); }, after: loadScreen,
  });
};

// ---- integrations -------------------------------------------------------------

function screenIntegrations() {
  var inProject = S.m.screen === 'project';
  var out = head('integrations', '<button type="button" class="btn" data-act="rerun" data-k="rescan">' + UI.icon('refresh') + esc(t('gui.btn.scan')) + '</button>') + busyLine() +
    '<p class="hint">' + esc(t('gui.int.scope')) + ': ' + esc(t(inProject ? 'gui.profile.scope.project' : 'gui.profile.scope.global')) + '</p>';
  (S.m.integrations || []).forEach(function (it) {
    var b = it.managed ? (it.healthy ? badge('ok', t('gui.int.connected')) : badge('warn', t('gui.int.modified'))) : (it.usable ? badge('todo', t('gui.int.unconnected')) : badge('err', t('gui.int.unavailable')));
    var acts = '';
    if (it.managed && !it.healthy) acts += iact(it, 'apply', 'gui.int.repair', true);
    if (it.managed) acts += iact(it, 'apply', 'gui.int.reconfigure') + iact(it, 'remove', 'gui.int.remove', false, true);
    else if (it.usable) acts += iact(it, 'apply', 'gui.int.connect', true);
    out += '<div class="card"><div class="head"><span class="av" aria-hidden="true">' + UI.icon(rtIcon(it.name), 'lg') + '</span><b>' + esc(it.label) + '</b>' + b + '<span class="sp"></span><div class="acts">' + acts + '</div></div>' +
      (it.problem ? '<div class="meta"><span>' + esc(it.problem) + '</span></div>' : '') + '</div>';
  });
  return out;
}

var INT_POP = { 'gui.int.repair': 'int-repair', 'gui.int.remove': 'int-remove', 'gui.int.reconfigure': 'int-connect', 'gui.int.connect': 'int-connect' };

function iact(it, op, key, primary, danger) {
  return '<button type="button" class="btn' + (primary ? ' pri' : '') + (danger ? ' dng' : '') + '"' + UI.popAttr(INT_POP[key]) + ' data-act="int" data-name="' + esc(it.name) + '" data-op="' + op + '" data-k="int-' + esc(it.name) + '-' + op + (primary ? '-p' : '') + '">' + esc(t(key)) + '</button>';
}

actions.int = function (el) {
  var name = el.getAttribute('data-name');
  var op = el.getAttribute('data-op');
  var it = S.m.integrations.filter(function (i) { return i.name === name; })[0];
  var req = { root: PR(), runtime: name, op: op, scope: S.m.screen === 'project' ? 'project' : 'global', memory: '', migratePersona: false };
  planAndRun({
    title: it.label, plan: function () { return api().PlanIntegration(req); }, run: function (id) { return api().ApplyIntegration(req, id); },
    danger: op === 'remove', confirmLabel: op === 'remove' ? t('gui.int.remove') : t('gui.btn.apply'), after: loadScreen,
  });
};

// ---- persona --------------------------------------------------------------------

function screenPersona() {
  var f = S.m.f;
  return head('persona') + busyLine() +
    '<div class="lbl">' + UI.icon('globe') + esc(t('gui.welcome.language')) + '</div>' +
    '<div class="grid2">' + radio('m.f.lang', 'es', t('gui.persona.lang.es'), '', { icon: 'globe' }) + radio('m.f.lang', 'en', t('gui.persona.lang.en'), '', { icon: 'globe' }) + '</div>' +
    '<div class="lbl">' + UI.icon('chat') + esc(t('gui.profile.mode')) + '</div>' + personaCards('m.f.mode') + '<p class="hint">' + esc(t('gui.profile.mode.hint')) + '</p>' +
    '<div class="note">' + UI.icon('info') + '<span>' + esc(t('gui.persona.note')) + '</span></div>' +
    '<div><button type="button" class="btn pri" data-act="persona" data-k="persona-save">' + UI.icon('check') + esc(t('gui.btn.save')) + '</button></div>';
}

actions.persona = function () {
  var f = S.m.f;
  planAndRun({
    title: t('gui.mgr.nav.persona'), confirmLabel: t('gui.btn.save'),
    plan: summaryPlan(t('gui.mgr.nav.persona'), [t('gui.welcome.language') + ': ' + f.lang, t('gui.profile.mode') + ': ' + f.mode]),
    run: async function () {
      var r = await api().SetPersona('', f.lang, f.mode);
      if (f.lang !== S.lang) {
        S.lang = f.lang;
        I18n.set(await api().Messages(f.lang));
      }
      return r;
    },
  });
};

// ---- memory -------------------------------------------------------------------

function screenMemory() {
  var m = S.m.f.mem;
  var team = m.mode === 'team';
  var out = head('memory') + busyLine() + engramBlock() +
    '<div class="grid2">' + radio('m.f.mem.mode', 'solo', t('gui.memory.solo'), t('gui.memory.solo.desc'), { icon: 'laptop', pop: 'mem-solo' }) + radio('m.f.mem.mode', 'team', t('gui.memory.team'), t('gui.memory.team.desc'), { icon: 'users', pop: 'mem-team' }) + '</div>';
  if (team) {
    out += '<div class="panel"><div class="field">' + esc(t('gui.memory.role')) + seg('m.f.mem.role', [['host', 'gui.memory.role.host'], ['client', 'gui.memory.role.client']], t('gui.memory.role')) + '</div>';
    if (m.role === 'host') {
      out += '<div class="grid3"><label class="field">' + esc(t('gui.memory.engine')) + '<select data-bind="m.f.mem.engine" data-k="m.f.mem.engine">' +
        [['auto', 'gui.memory.engine.auto'], ['docker', 'gui.memory.engine.docker'], ['podman', 'gui.memory.engine.podman']].map(function (o) { return '<option value="' + o[0] + '"' + attr('selected', true, m.engine === o[0]) + '>' + esc(t(o[1])) + '</option>'; }).join('') + '</select></label>' +
        field('m.f.mem.port', t('gui.memory.port'), { mono: true, placeholder: t('gui.memory.port.auto') }) + field('m.f.mem.instance', t('gui.memory.instance'), { placeholder: 'default' }) + '</div>' +
        field('m.f.mem.url', t('gui.memory.url'), { mono: true, hint: t('gui.memory.url.hint') });
    } else {
      out += field('m.f.mem.url', t('gui.memory.url.client'), { mono: true }) + field('m.f.mem.tokenEnv', t('gui.memory.token'), { mono: true, placeholder: 'MEMORY_TOKEN', hint: t('gui.memory.token.hint') });
    }
    out += check('m.f.mem.importEngram', t('gui.memory.import')) + '</div>';
  }
  return out + '<div><button type="button" class="btn pri" data-act="memory" data-k="mem-apply">' + esc(t('gui.btn.apply')) + '</button></div>';
}

// engramBlock shows whether Engram is installed and offers to install it.
function engramBlock() {
  var g = S.m.eng;
  if (!g) return '';
  var out = '<div class="box"><div class="row"><span class="oic sm">' + UI.icon('db') + '</span><b>' + esc(t('gui.eng.title')) + '</b>' +
    (g.installed ? '<span class="mono">' + esc(g.version || '') + '</span>' + badge('ok', t('gui.cg.registered')) : badge('warn', t('gui.eng.missing'))) + '</div>' +
    (g.path ? '<div class="row"><b>' + esc(t('gui.eng.path')) + '</b><span class="mono">' + esc(g.path) + '</span></div>' : '') +
    '<div class="row"><b>' + esc(t('gui.eng.pinned')) + '</b><span class="mono">' + esc(g.pinned) + '</span></div>' +
    '<div class="row"><b>' + esc(t('gui.eng.source')) + '</b><span class="mono">' + esc(g.source) + '</span></div></div>';
  if (g.needsWork) {
    out += '<div class="note" role="status">' + UI.icon('info') + '<span>' + esc(t('gui.eng.need')) + '</span></div>' +
      '<div><button type="button" class="btn pri" data-act="engfix" data-k="eng-fix"' + UI.popAttr('eng-fix') + '>' + UI.icon('wrench') + esc(t('gui.eng.act.install')) + '</button></div>';
  } else if (g.installed) {
    out += '<p class="hint">' + esc(t('gui.eng.ok')) + '</p>';
  }
  return out;
}

actions.engfix = function () {
  planAndRun({
    title: t('gui.eng.title'), plan: function () { return api().PlanEngram(); },
    run: async function (id) { var steps = await api().ApplyEngram(id); return { steps: steps }; },
  });
};

function memoryAnswers() {
  var m = S.m.f.mem;
  var team = m.mode === 'team', host = team && m.role === 'host', client = team && m.role === 'client';
  return {
    mode: m.mode, role: team ? m.role : '', engine: host ? m.engine : '', port: host ? parseInt(m.port, 10) || 0 : 0,
    instance: host ? m.instance.trim() : '', url: team ? m.url.trim() : '', tokenEnv: client ? m.tokenEnv.trim() : '',
    storeToken: false, importEngram: team && !!m.importEngram, invites: [],
  };
}

actions.memory = function () {
  var a = memoryAnswers();
  if (a.mode === 'team' && a.role === 'client' && !a.url) return toast(t('gui.memory.url.required'));
  if (a.tokenEnv && !ENV_NAME.test(a.tokenEnv)) return toast(t('gui.memory.token.invalid'));
  planAndRun({
    title: t('gui.mgr.nav.memory'), plan: function () { return api().PlanMemory(S.m.proj, a); },
    run: async function (id) { var r = await api().ApplyMemory(S.m.proj, a, id); return r; },
  });
};

// ---- guard hooks (project tab) --------------------------------------------------------------

actions.hooks = function (el) {
  var install = el.getAttribute('data-val') === 'install';
  planAndRun({
    title: t('gui.windows.guard'), confirmLabel: t('gui.btn.apply'),
    plan: summaryPlan(t('gui.windows.guard'), [install ? t('gui.guard.install') : t('gui.guard.remove')]),
    run: async function () { await api().SetGuardHooks(S.m.proj, install); return { message: t('gui.modal.done') }; },
  });
};

actions.scanhist = function () {
  var req = { root: S.m.proj, id: 'guard.scan-history', args: {} };
  planAndRun({ title: t('gui.guard.history'), plan: function () { return api().PlanFix(req); }, run: function (id) { return api().RunFix(req, id); }, after: function () {} });
};

// ---- backups --------------------------------------------------------------------

function screenBackups() {
  var btns = '<button type="button" class="btn" data-act="cleanup"' + UI.popAttr('cleanup') + ' data-k="cleanup">' + UI.icon('trash') + esc(t('gui.bk.cleanup')) + '</button>';
  var out = head('backups', btns) + busyLine();
  var snaps = (S.m.snaps || []).slice().reverse();
  if (!snaps.length && !S.m.busy) return out + UI.emptyState('archive', t('gui.bk.empty'), '');
  out += '<div class="box tbl">';
  snaps.forEach(function (s, i) {
    out += '<div class="row"><span class="oic sm">' + UI.icon('archive') + '</span><b style="width:auto;flex:1;min-width:0;overflow-wrap:anywhere">' + esc(s.id) + '</b><span class="mono">' + esc(s.createdAt) + '</span>' +
      badge(s.status === 'ok' || s.status === 'complete' ? 'ok' : 'todo', s.status || '-') + '<span class="mono">' + esc(t('gui.bk.files', { count: s.files })) + '</span>' +
      '<button type="button" class="btn xs" data-act="restore"' + UI.popAttr('restore') + ' data-id="' + esc(s.id) + '" data-k="restore-' + i + '">' + UI.icon('undo') + esc(t('gui.bk.restore')) + '</button></div>';
  });
  return out + '</div>';
}

actions.restore = function (el) {
  var id = el.getAttribute('data-id');
  planAndRun({
    title: t('gui.bk.restore') + ': ' + id, confirmLabel: t('gui.bk.restore'), danger: true,
    plan: function () { return api().PlanRestore(PR(), id); }, run: function (pid) { return api().Restore(PR(), id, pid); },
  });
};

actions.cleanup = function () {
  planAndRun({
    title: t('gui.bk.cleanup'), confirmLabel: t('gui.bk.cleanup'), danger: true,
    plan: function () { return api().PlanCleanup(PR()); },
    run: async function (pid) { return { removed: await api().ApplyCleanup(PR(), pid) }; },
  });
};

// ---- uninstall ------------------------------------------------------------------

function managedRuntimes() { return (S.m.integrations || []).filter(function (i) { return i.managed; }); }

function allRuntimesSelected() {
  return managedRuntimes().every(function (i) { return S.m.un.runtimes[i.name]; });
}

function screenUninstall() {
  var u = S.m.un;
  var d = S.m.settings && S.m.settings.desktop;
  var all = allRuntimesSelected();
  var rows = managedRuntimes().map(function (i) {
    return '<div class="tog' + (u.runtimes[i.name] ? ' sel' : '') + '"><label class="tgl"><input type="checkbox" data-bind="m.un.runtimes.' + esc(i.name) + '" data-k="un-' + esc(i.name) + '"' + attr('checked', true, !!u.runtimes[i.name]) + '><span class="oic sm">' + UI.icon(rtIcon(i.name)) + '</span><b>' + esc(i.label) + '</b><span>' + esc(t('gui.un.runtime.desc')) + '</span></label></div>';
  }).join('');
  if (!rows) rows = '<div class="tog"><span class="tgl">' + esc(t('gui.un.no-runtimes')) + '</span></div>';
  var hasProgram = !!(d && d.supported && d.installed);
  return head('uninstall') + busyLine() + '<div class="lbl">' + esc(t('gui.un.what')) + '</div><div class="box">' + rows +
    '<div class="tog' + (all ? '' : ' off') + '"' + attr('title', t('gui.un.guard.needs-all'), !all) + '><label class="tgl"><input type="checkbox" data-bind="m.un.guard" data-k="un-guard"' + attr('checked', true, !!u.guard && all) + attr('disabled', true, !all) + '><span class="oic sm">' + UI.icon('shield') + '</span><b>' + esc(t('gui.windows.guard')) + '</b><span>' + esc(t('gui.un.guard.desc')) + '</span></label></div>' +
    '<div class="tog' + (hasProgram ? '' : ' off') + '"><label class="tgl"><input type="checkbox" data-bind="m.un.program" data-k="un-program"' + attr('checked', true, !!u.program && hasProgram) + attr('disabled', true, !hasProgram) + '><span class="oic sm">' + UI.icon('desktop') + '</span><b>' + esc(t('gui.un.program')) + '</b><span>' + esc(t('gui.un.program.desc')) + '</span></label></div></div>' +
    '<label class="danger"><input type="checkbox" data-bind="m.un.server" data-k="un-server"' + attr('checked', true, !!u.server) + '>' + UI.icon('warn', 'lg') + '<span><b>' + esc(t('gui.un.server')) + '</b> ' + esc(t('gui.un.server.warn')) + '</span></label>' +
    '<div class="note">' + UI.icon('info') + '<span>' + esc(t('gui.un.keep')) + ' <a href="#" data-act="go" data-val="backups">' + esc(t('gui.mgr.nav.backups')) + '</a></span></div>' +
    '<div style="display:flex;gap:10px;justify-content:flex-end;margin-top:auto"><button type="button" class="btn" data-act="go" data-val="overview" data-k="un-cancel">' + esc(t('gui.btn.cancel')) + '</button>' +
    '<button type="button" class="btn dng fill" data-act="uninstall"' + UI.popAttr('uninstall') + ' data-k="un-go">' + UI.icon('trash') + esc(t('gui.un.go')) + '</button></div>';
}

function uninstallOptions() {
  var u = S.m.un;
  var d = S.m.settings && S.m.settings.desktop;
  var all = allRuntimesSelected();
  var selected = managedRuntimes().filter(function (i) { return u.runtimes[i.name]; }).map(function (i) { return i.name; });
  var wantGuard = !!u.guard && all;
  var names = [];
  if (!(all && wantGuard)) names = selected.length ? selected : ['-none-'];
  return {
    root: '', runtimes: names.indexOf('-none-') >= 0 ? [] : names, removeMemoryServer: !!u.server, purgeMemoryData: !!u.server, purgeConfirm: '',
    desktop: !!(u.program && d && d.supported && d.installed), installDir: d ? d.installDir : '',
    _nothing: names.indexOf('-none-') >= 0 && !wantGuard,
  };
}

actions.uninstall = function () {
  var o = uninstallOptions();
  if (o._nothing && !o.desktop && !o.removeMemoryServer) return toast(t('gui.un.nothing'));
  delete o._nothing;
  S.m.purge = '';
  planAndRun({
    title: t('gui.un.title'), confirmLabel: t('gui.un.go'), danger: true,
    plan: async function () {
      var p = await api().PlanUninstall(o);
      p.keep = [t('gui.un.keep')];
      return p;
    },
    extra: function (pl) {
      return pl.purgeVolume ? '<p class="alert" style="text-align:left">' + esc(t('gui.un.purge.type', { name: pl.purgeVolume })) + '</p>' + field('m.purge', t('gui.un.purge.field'), { mono: true }) : '';
    },
    check: function (pl) { return pl.purgeVolume && S.m.purge !== pl.purgeVolume ? t('gui.un.purge.mismatch') : ''; },
    run: function (id) { o.purgeConfirm = S.m.purge; return api().Uninstall(o, id); },
    quitAfter: function () { return o.desktop; },
  });
};
