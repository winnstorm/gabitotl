'use strict';
// manager.js: the GabitoTL Manager (Overview, Doctor, Integrations, Persona,
// Memory, Delivery, Guard & code index, Backups, Uninstall). Every change goes
// through planAndRun: the plan is shown in a modal before anything is applied.

var NAV = [
  ['overview', 'gui.mgr.nav.overview'],
  ['doctor', 'gui.mgr.nav.doctor'],
  ['#', 'gui.mgr.nav.settings'],
  ['integrations', 'gui.mgr.nav.integrations'],
  ['persona', 'gui.mgr.nav.persona'],
  ['memory', 'gui.mgr.nav.memory'],
  ['delivery', 'gui.mgr.nav.delivery'],
  ['guard', 'gui.mgr.nav.guard'],
  ['#', 'gui.mgr.nav.maintenance'],
  ['backups', 'gui.mgr.nav.backups'],
  ['uninstall', 'gui.mgr.nav.uninstall'],
];

S.m = {
  screen: 'overview', busy: false, doctor: null, integrations: null, settings: null, snaps: null,
  scope: {}, toast: '', modal: null, loadErr: '',
  f: {
    lang: 'es', mode: 'normal',
    mem: { mode: 'solo', role: 'host', engine: 'auto', port: '', instance: '', url: '', tokenEnv: '', importEngram: false },
    del: { provider: 'none', automation: 'until-pr', mergeMethod: 'squash', deleteBranch: true, draft: true, tokenEnv: '' },
  },
  un: { runtimes: {}, guard: true, program: true, server: false },
  purge: '',
};

var NEEDS_PROJECT = { memory: true, delivery: true, guard: true };

async function managerBoot() {
  try { S.root = (await api().Root()) || ''; } catch (e) { S.root = ''; }
  S.m.screen = S.info.route === 'uninstall' ? 'uninstall' : 'overview';
  await loadScreen();
}

async function managerReload() { await loadScreen(); }

function projectName() {
  if (!S.root) return t('gui.mgr.project.none');
  return S.root;
}

// ---- data loading -------------------------------------------------------------

async function loadScreen() {
  S.m.loadErr = '';
  S.m.busy = true;
  render();
  try {
    var sc = S.m.screen;
    if (sc === 'overview' || sc === 'doctor') S.m.doctor = await api().Doctor(S.root);
    if (sc === 'overview' || sc === 'integrations' || sc === 'uninstall') S.m.integrations = await api().Integrations(S.root);
    if (sc !== 'doctor' && sc !== 'integrations' && sc !== 'backups') {
      S.m.settings = await api().Settings(S.root);
      seedForms(S.m.settings);
    }
    if (sc === 'backups') S.m.snaps = await api().Snapshots(S.root);
  } catch (e) {
    S.m.loadErr = t('gui.error.generic', { detail: errText(e) });
  }
  S.m.busy = false;
  render();
}

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
  var d = st.delivery;
  if (d && d.configured) {
    f.del.provider = ['github', 'gitlab', 'bitbucket'].indexOf(d.provider) >= 0 ? d.provider : 'none';
    f.del.automation = d.automation || 'until-pr';
    f.del.mergeMethod = d.mergeMethod || 'squash';
    f.del.deleteBranch = !!d.deleteBranch;
    f.del.draft = !!d.draft;
    f.del.tokenEnv = (d.auth && d.auth.indexOf('env:') === 0) ? d.auth.slice(4) : '';
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

actions.chroot = async function () {
  try {
    var dir = await api().ChooseFolder(t('gui.detect.project'), S.root);
    if (dir) {
      S.root = dir;
      await api().SetRoot(dir);
      await loadScreen();
    }
  } catch (e) {
    toast(t('gui.error.generic', { detail: errText(e) }));
  }
};

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
  if (!m || m.busy) return;
  var a = m.actions[parseInt(el.getAttribute('data-i'), 10)];
  if (a && a.fn) a.fn();
};

function modalHTML() {
  var m = S.m.modal;
  if (!m) return '';
  var acts = (m.actions || []).map(function (a, i) {
    return '<button type="button" class="btn' + (a.primary ? ' pri' : '') + (a.danger ? ' dng fill' : '') + '" data-act="mact" data-i="' + i + '" data-k="modal-' + i + '"' + attr('disabled', true, !!m.busy) + '>' + esc(a.label) + '</button>';
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
// confirmLabel, danger, extra (html), check() (returns an error text or '').
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
    mm.actions = [];
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

function renderManager() {
  var nav = NAV.map(function (n) {
    if (n[0] === '#') return '<div class="grp">' + esc(t(n[1])) + '</div>';
    return '<button type="button" class="nav" data-act="go" data-val="' + n[0] + '" data-k="nav-' + n[0] + '"' + (S.m.screen === n[0] ? ' aria-current="page"' : '') + '>' + esc(t(n[1])) + '</button>';
  }).join('');
  var side = '<nav class="side" aria-label="' + esc(t('gui.mgr.nav.label')) + '">' + nav +
    '<div class="foot mono"><span>v' + esc(S.info.version) + '</span><span>' + esc(projectName()) + '</span>' +
    '<button type="button" data-act="chroot" data-k="chroot">' + esc(t('gui.btn.change')) + '</button></div></nav>';
  var body = S.m.loadErr ? '<p class="lead" role="alert">' + esc(S.m.loadErr) + '</p>' : screenHTML();
  return '<div id="mgr" style="display:flex;flex-direction:column;flex:1;min-height:0">' + titlebar('gui.mgr.title') + '<div class="body">' + side +
    '<main class="pane" id="pane">' + body + '</main></div></div>' + modalHTML() +
    (S.m.toast ? '<div class="toast" role="status">' + esc(S.m.toast) + '</div>' : '');
}

function screenHTML() {
  var sc = S.m.screen;
  if (NEEDS_PROJECT[sc] && !S.root) return head(sc) + needsProject();
  switch (sc) {
    case 'overview': return screenOverview();
    case 'doctor': return screenDoctor();
    case 'integrations': return screenIntegrations();
    case 'persona': return screenPersona();
    case 'memory': return screenMemory();
    case 'delivery': return screenDelivery();
    case 'guard': return screenGuard();
    case 'backups': return screenBackups();
    case 'uninstall': return screenUninstall();
  }
  return '';
}

var TITLES = {
  overview: ['gui.mgr.nav.overview', 'gui.ov.lead'], doctor: ['gui.doc.title', 'gui.doc.lead'],
  integrations: ['gui.mgr.nav.integrations', 'gui.int.lead'], persona: ['gui.mgr.nav.persona', 'gui.persona.lead'],
  memory: ['gui.mgr.nav.memory', 'gui.memory.lead'], delivery: ['gui.mgr.nav.delivery', 'gui.delivery.lead'],
  guard: ['gui.mgr.nav.guard', 'gui.guard.lead'], backups: ['gui.mgr.nav.backups', 'gui.bk.lead'],
  uninstall: ['gui.un.title', 'gui.un.lead'],
};

function head(sc, buttons) {
  var tl = TITLES[sc];
  return '<div class="hrow"><div class="grow"><h1>' + esc(t(tl[0])) + '</h1><p class="lead">' + esc(t(tl[1])) + '</p></div>' + (buttons || '') + '</div>';
}

function needsProject() {
  return '<div class="note"><span aria-hidden="true">ⓘ</span><span>' + esc(t('gui.mgr.needs-project')) + '</span></div>' +
    '<div><button type="button" class="btn pri" data-act="chroot" data-k="needs-root">' + esc(t('gui.btn.change')) + '</button></div>';
}

function busyLine() { return S.m.busy ? '<p class="lead">' + esc(t('gui.loading')) + '</p>' : ''; }

// ---- overview ---------------------------------------------------------------------

function counts(rep) {
  var c = { ok: 0, warn: 0, todo: 0, error: 0 };
  ((rep && rep.sections) || []).forEach(function (s) { s.lines.forEach(function (l) { if (c[l.status] !== undefined) c[l.status]++; }); });
  return c;
}

function stat(n, key) { return '<div class="stat"><b>' + esc(n) + '</b>' + esc(t(key)) + '</div>'; }

function screenOverview() {
  var c = counts(S.m.doctor);
  var ints = S.m.integrations || [];
  var managed = ints.filter(function (i) { return i.managed; }).length;
  var st = S.m.settings;
  var mem = !st || !st.memory.configured ? t('gui.ov.memory.none') : (st.memory.mode === 'team' ? t('gui.ov.memory.team') : t('gui.memory.solo'));
  var out = head('overview') + busyLine() +
    '<div class="stats">' + stat(c.ok, 'gui.doc.ok') + stat(c.warn, 'gui.doc.warn') + stat(c.todo, 'gui.doc.todo') + stat(c.error, 'gui.doc.error') + '</div>' +
    '<dl class="sum"><dt>' + esc(t('gui.ov.version')) + '</dt><dd>' + esc(S.info.version) + '</dd>' +
    '<dt>' + esc(t('gui.mgr.nav.integrations')) + '</dt><dd>' + esc(t('gui.ov.connected', { n: managed, total: ints.length })) + '</dd>' +
    '<dt>' + esc(t('gui.mgr.nav.memory')) + '</dt><dd>' + esc(mem) + '</dd>' +
    '<dt>' + esc(t('gui.detect.project')) + '</dt><dd class="mono">' + esc(projectName()) + '</dd></dl>' +
    '<div class="lbl">' + esc(t('gui.ov.actions')) + '</div><div class="acts">' +
    '<button type="button" class="btn" data-act="go" data-val="doctor" data-k="qa-doctor">' + esc(t('gui.ov.run-doctor')) + '</button>' +
    '<button type="button" class="btn" data-act="go" data-val="integrations" data-k="qa-int">' + esc(t('gui.ov.connect')) + '</button>' +
    '<button type="button" class="btn" data-act="go" data-val="backups" data-k="qa-bk">' + esc(t('gui.ov.backups')) + '</button></div>';
  return out + desktopCard();
}

function desktopCard() {
  var d = S.m.settings && S.m.settings.desktop;
  if (!d || !d.supported) return '';
  var rows = [['startMenu', 'gui.windows.startmenu', d.startMenu], ['desktopShortcut', 'gui.windows.desktop', d.desktopShortcut], ['addToPath', 'gui.windows.path', d.onPath]];
  return '<div class="lbl">' + esc(t('gui.ov.windows')) + '</div><div class="box"><div class="row"><b>' + esc(t('gui.windows.dir')) + '</b><span class="mono">' + esc(d.installDir) + '</span></div>' +
    rows.map(function (r) {
      return '<div class="row"><b>' + esc(t(r[1])) + '</b>' + (r[2] ? badge('ok', t('gui.ov.in-place')) : badge('todo', t('gui.ov.missing'))) + '<span class="sp"></span>' +
        (r[2] ? '' : '<button type="button" class="btn xs" data-act="desk" data-val="' + r[0] + '" data-k="desk-' + r[0] + '">' + esc(t('gui.ov.add')) + '</button>') + '</div>';
    }).join('') + '</div>';
}

actions.desk = function (el) {
  var d = S.m.settings.desktop;
  var w = { installDir: d.installDir, startMenu: d.startMenu, desktopShortcut: d.desktopShortcut, addToPath: d.onPath, guardHooks: false, codeIndex: '' };
  w[el.getAttribute('data-val')] = true;
  planAndRun({ title: t('gui.ov.windows'), plan: function () { return api().PlanDesktop(w); }, run: function (id) { return api().ApplyDesktop(w, id); } });
};

// ---- doctor -------------------------------------------------------------------

var ICONS = { ok: ['ok', '✓', 'gui.doc.ok'], warn: ['warn', '!', 'gui.doc.warn'], todo: ['todo', '→', 'gui.doc.todo'], error: ['err', '×', 'gui.doc.error'] };

function screenDoctor() {
  var rep = S.m.doctor;
  var btns = '<button type="button" class="btn" data-act="copy" data-k="copy"' + attr('disabled', true, !rep) + '>' + esc(t('gui.doc.copy')) + '</button>' +
    '<button type="button" class="btn pri" data-act="rerun" data-k="rerun"' + attr('disabled', true, S.m.busy) + '>' + esc(t('gui.doc.rerun')) + '</button>';
  var out = head('doctor', btns) + busyLine();
  if (!rep) return out;
  var c = counts(rep);
  out += '<div class="stats">' + stat(c.ok, 'gui.doc.ok') + stat(c.warn, 'gui.doc.warn') + stat(c.todo, 'gui.doc.todo') + stat(c.error, 'gui.doc.error') + '</div><div class="list">';
  rep.sections.forEach(function (sec, si) {
    out += '<div class="sec">' + esc(sec.title) + '</div>';
    sec.lines.forEach(function (l, li) {
      var ic = ICONS[l.status] || ICONS.ok;
      out += '<div class="row"><span class="ic ' + ic[0] + '" role="img" aria-label="' + esc(t(ic[2])) + '">' + ic[1] + '</span><span>' + esc(l.message) + '</span><span class="sp"></span>' +
        (l.detail && l.detail !== l.message ? '<span class="mono">' + esc(l.detail) + '</span>' : '') +
        (l.fix ? '<button type="button" class="btn xs" data-act="fix" data-s="' + si + '" data-l="' + li + '" data-k="fix-' + si + '-' + li + '">' + esc(l.fix.label) + '</button>' : '') + '</div>';
    });
  });
  return out + '</div>';
}

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
  var req = { root: S.root, id: l.fix.id, args: l.fix.args || {} };
  planAndRun({
    title: l.fix.label, plan: function () { return api().PlanFix(req); },
    run: function (id) { return api().RunFix(req, id); }, after: loadScreen,
  });
};

// ---- integrations -------------------------------------------------------------

function initials(label) {
  var w = String(label).split(/\s+/);
  return (w.length > 1 ? w[0][0] + w[1][0] : String(label).slice(0, 2)).toUpperCase();
}

function screenIntegrations() {
  var out = head('integrations', '<button type="button" class="btn" data-act="rerun" data-k="rescan">' + esc(t('gui.btn.scan')) + '</button>') + busyLine();
  (S.m.integrations || []).forEach(function (it) {
    var b = it.managed ? (it.healthy ? badge('ok', t('gui.int.connected')) : badge('warn', t('gui.int.modified'))) : (it.usable ? badge('todo', t('gui.int.unconnected')) : badge('err', t('gui.int.unavailable')));
    var scope = S.m.scope[it.name] || 'global';
    var canProject = !!S.root;
    var acts = '';
    if (it.managed && !it.healthy) acts += iact(it, 'apply', 'gui.int.repair', true);
    if (it.managed) acts += iact(it, 'apply', 'gui.int.reconfigure') + iact(it, 'remove', 'gui.int.remove', false, true);
    else if (it.usable) acts += iact(it, 'apply', 'gui.int.connect', true);
    var sel = (it.usable || it.managed) ? '<label class="hint">' + esc(t('gui.int.scope')) + ' <select class="sel-sm" data-bind="m.scope.' + esc(it.name) + '" data-k="scope-' + esc(it.name) + '" data-norender>' +
      '<option value="global"' + attr('selected', true, scope === 'global') + '>' + esc(t('gui.profile.scope.global')) + '</option>' +
      '<option value="project"' + attr('selected', true, scope === 'project') + attr('disabled', true, !canProject) + (canProject ? '' : ' title="' + esc(t('gui.mgr.needs-project')) + '"') + '>' + esc(t('gui.profile.scope.project')) + '</option></select></label>' : '';
    out += '<div class="card"><div class="head"><span class="av" aria-hidden="true">' + esc(initials(it.label)) + '</span><b>' + esc(it.label) + '</b>' + b + '<span class="sp"></span><div class="acts">' + sel + acts + '</div></div>' +
      (it.problem ? '<div class="meta"><span>' + esc(it.problem) + '</span></div>' : '') + '</div>';
  });
  return out;
}

function iact(it, op, key, primary, danger) {
  return '<button type="button" class="btn' + (primary ? ' pri' : '') + (danger ? ' dng' : '') + '" data-act="int" data-name="' + esc(it.name) + '" data-op="' + op + '" data-k="int-' + esc(it.name) + '-' + op + (primary ? '-p' : '') + '">' + esc(t(key)) + '</button>';
}

actions.int = function (el) {
  var name = el.getAttribute('data-name');
  var op = el.getAttribute('data-op');
  var it = S.m.integrations.filter(function (i) { return i.name === name; })[0];
  var req = { root: S.root, runtime: name, op: op, scope: S.m.scope[name] || 'global', memory: '', migratePersona: false };
  planAndRun({
    title: it.label, plan: function () { return api().PlanIntegration(req); }, run: function (id) { return api().ApplyIntegration(req, id); },
    danger: op === 'remove', confirmLabel: op === 'remove' ? t('gui.int.remove') : t('gui.btn.apply'), after: loadScreen,
  });
};

// ---- persona --------------------------------------------------------------------

function screenPersona() {
  var f = S.m.f;
  return head('persona') + busyLine() +
    '<div class="field">' + esc(t('gui.welcome.language')) + seg('m.f.lang', [['es', 'gui.persona.lang.es'], ['en', 'gui.persona.lang.en']], t('gui.welcome.language')) + '</div>' +
    '<div class="field">' + esc(t('gui.profile.mode')) + seg('m.f.mode', [['lite', 'gui.profile.mode.lite'], ['normal', 'gui.profile.mode.normal'], ['full', 'gui.profile.mode.full']], t('gui.profile.mode')) + '<span class="hint">' + esc(t('gui.profile.mode.hint')) + '</span></div>' +
    '<div class="note"><span aria-hidden="true">ⓘ</span><span>' + esc(t('gui.persona.note')) + '</span></div>' +
    '<div><button type="button" class="btn pri" data-act="persona" data-k="persona-save">' + esc(t('gui.btn.save')) + '</button></div>';
}

actions.persona = function () {
  var f = S.m.f;
  planAndRun({
    title: t('gui.mgr.nav.persona'), confirmLabel: t('gui.btn.save'),
    plan: summaryPlan(t('gui.mgr.nav.persona'), [t('gui.welcome.language') + ': ' + f.lang, t('gui.profile.mode') + ': ' + f.mode]),
    run: async function () {
      var r = await api().SetPersona(S.root, f.lang, f.mode);
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
  var out = head('memory') + busyLine() +
    '<div class="grid2">' + radio('m.f.mem.mode', 'solo', t('gui.memory.solo'), t('gui.memory.solo.desc')) + radio('m.f.mem.mode', 'team', t('gui.memory.team'), t('gui.memory.team.desc')) + '</div>';
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
    title: t('gui.mgr.nav.memory'), plan: function () { return api().PlanMemory(S.root, a); },
    run: async function (id) { var r = await api().ApplyMemory(S.root, a, id); return r; },
  });
};

// ---- delivery -------------------------------------------------------------------

function screenDelivery() {
  var dl = S.m.f.del;
  var all = dl.automation === 'all';
  var out = head('delivery') + busyLine() +
    '<div class="field">' + esc(t('gui.delivery.platform')) + seg('m.f.del.provider', [['github', 'gui.delivery.platform.github'], ['gitlab', 'gui.delivery.platform.gitlab'], ['bitbucket', 'gui.delivery.platform.bitbucket'], ['none', 'gui.delivery.platform.none']], t('gui.delivery.platform')) + '</div>' +
    '<div class="field">' + esc(t('gui.delivery.automation')) + '<div class="grid3">' +
    radio('m.f.del.automation', 'all', t('gui.delivery.auto.all'), t('gui.delivery.auto.all.desc')) +
    radio('m.f.del.automation', 'until-pr', t('gui.delivery.auto.until-pr'), t('gui.delivery.auto.until-pr.desc')) +
    radio('m.f.del.automation', 'ask', t('gui.delivery.auto.ask'), t('gui.delivery.auto.ask.desc')) + '</div></div>' +
    '<div class="grid2" style="align-items:end"><label class="field">' + esc(t('gui.delivery.merge')) + '<select data-bind="m.f.del.mergeMethod" data-k="m.f.del.mergeMethod"' + attr('disabled', true, !all) + '>' +
    [['squash', 'gui.delivery.merge.squash'], ['merge', 'gui.delivery.merge.merge'], ['rebase', 'gui.delivery.merge.rebase']].map(function (o) { return '<option value="' + o[0] + '"' + attr('selected', true, dl.mergeMethod === o[0]) + '>' + esc(t(o[1])) + '</option>'; }).join('') +
    '</select><span class="hint">' + esc(t('gui.delivery.merge.hint')) + '</span></label><div style="display:flex;flex-direction:column;gap:10px;padding-bottom:20px">' +
    check('m.f.del.draft', t('gui.delivery.draft')) + check('m.f.del.deleteBranch', t('gui.delivery.delete-branch'), { disabled: !all, title: all ? '' : t('gui.delivery.merge.hint') }) + '</div></div>';
  if (dl.provider !== 'none') out += field('m.f.del.tokenEnv', t('gui.delivery.token'), { mono: true, placeholder: PROVIDER_ENV[dl.provider] || '' });
  return out + '<div><button type="button" class="btn pri" data-act="delivery" data-k="del-save">' + esc(t('gui.btn.save')) + '</button></div>';
}

actions.delivery = function () {
  var d = S.m.f.del;
  var all = d.automation === 'all';
  if (d.provider !== 'none' && d.tokenEnv && !ENV_NAME.test(d.tokenEnv)) return toast(t('gui.delivery.token.invalid'));
  var ans = { provider: d.provider === 'none' ? '' : d.provider, automation: d.automation, mergeMethod: all ? d.mergeMethod : '', deleteBranch: all ? !!d.deleteBranch : null, draft: !!d.draft, tokenEnv: d.provider === 'none' ? '' : d.tokenEnv.trim() };
  planAndRun({
    title: t('gui.mgr.nav.delivery'), confirmLabel: t('gui.btn.save'),
    plan: summaryPlan(t('gui.mgr.nav.delivery'), [t('gui.delivery.platform') + ': ' + d.provider, t('gui.delivery.automation') + ': ' + d.automation, t('gui.delivery.draft') + ': ' + !!d.draft]),
    run: function () { return api().SetDelivery(S.root, ans); },
  });
};

// ---- guard & code index -----------------------------------------------------------

function screenGuard() {
  var st = S.m.settings;
  if (!st) return head('guard') + busyLine();
  var g = st.guard;
  var out = head('guard') + busyLine() + '<div class="lbl">' + esc(t('gui.windows.guard')) + '</div><div class="box"><div class="row"><b>' + esc(t('gui.guard.hooks')) + '</b>' +
    (g.hooksInstalled ? badge('ok', t('gui.guard.active')) : badge('todo', t('gui.guard.inactive'))) + '<span class="sp"></span>' +
    '<button type="button" class="btn xs" data-act="hooks" data-val="' + (g.hooksInstalled ? 'remove' : 'install') + '" data-k="hooks">' + esc(g.hooksInstalled ? t('gui.guard.remove') : t('gui.guard.install')) + '</button></div>' +
    '<div class="row"><b>' + esc(t('gui.guard.history')) + '</b><span class="sp"></span><button type="button" class="btn xs" data-act="scanhist" data-k="scanhist">' + esc(t('gui.guard.scan')) + '</button></div></div>' +
    '<div class="lbl">' + esc(t('gui.windows.index')) + '</div><div class="box">';
  if (!st.codeIndex.length) out += '<div class="row">' + esc(t('gui.guard.index.none')) + '</div>';
  st.codeIndex.forEach(function (b, i) {
    out += '<div class="row"><b>' + esc(b.name) + '</b>' + (b.indexed ? badge('ok', t('gui.guard.indexed')) : (b.installed ? badge('todo', t('gui.guard.not-indexed')) : badge('warn', t('gui.guard.not-installed')))) +
      (!b.installed && b.installHint ? '<span class="mono">' + esc(b.installHint) + '</span>' : '') + '<span class="sp"></span>' +
      '<button type="button" class="btn xs" data-act="index" data-i="' + i + '" data-k="index-' + i + '"' + attr('disabled', true, !b.installed) + attr('title', t('gui.guard.not-installed'), !b.installed) + '>' + esc(t('gui.guard.setup')) + '</button></div>';
  });
  return out + '</div>';
}

actions.hooks = function (el) {
  var install = el.getAttribute('data-val') === 'install';
  planAndRun({
    title: t('gui.windows.guard'), confirmLabel: t('gui.btn.apply'),
    plan: summaryPlan(t('gui.windows.guard'), [install ? t('gui.guard.install') : t('gui.guard.remove')]),
    run: async function () { await api().SetGuardHooks(S.root, install); return { message: t('gui.modal.done') }; },
  });
};

actions.scanhist = function () {
  var req = { root: S.root, id: 'guard.scan-history', args: {} };
  planAndRun({ title: t('gui.guard.history'), plan: function () { return api().PlanFix(req); }, run: function (id) { return api().RunFix(req, id); }, after: function () {} });
};

actions.index = function (el) {
  var b = S.m.settings.codeIndex[+el.getAttribute('data-i')];
  planAndRun({
    title: t('gui.windows.index'), confirmLabel: t('gui.btn.apply'),
    plan: summaryPlan(t('gui.windows.index'), [t('gui.guard.setup') + ' ' + b.name]),
    run: async function () { await api().SetCodeIndex(S.root, b.name); return { message: t('gui.modal.done') }; },
  });
};

// ---- backups --------------------------------------------------------------------

function screenBackups() {
  var btns = '<button type="button" class="btn" data-act="cleanup" data-k="cleanup">' + esc(t('gui.bk.cleanup')) + '</button>';
  var out = head('backups', btns) + busyLine();
  var snaps = (S.m.snaps || []).slice().reverse();
  if (!snaps.length && !S.m.busy) return out + '<p class="lead">' + esc(t('gui.bk.empty')) + '</p>';
  out += '<div class="box tbl">';
  snaps.forEach(function (s, i) {
    out += '<div class="row"><b style="width:auto;flex:1;min-width:0;overflow-wrap:anywhere">' + esc(s.id) + '</b><span class="mono">' + esc(s.createdAt) + '</span>' +
      badge(s.status === 'ok' || s.status === 'complete' ? 'ok' : 'todo', s.status || '-') + '<span class="mono">' + esc(t('gui.bk.files', { count: s.files })) + '</span>' +
      '<button type="button" class="btn xs" data-act="restore" data-id="' + esc(s.id) + '" data-k="restore-' + i + '">' + esc(t('gui.bk.restore')) + '</button></div>';
  });
  return out + '</div>';
}

actions.restore = function (el) {
  var id = el.getAttribute('data-id');
  planAndRun({
    title: t('gui.bk.restore') + ': ' + id, confirmLabel: t('gui.bk.restore'), danger: true,
    plan: function () { return api().PlanRestore(S.root, id); }, run: function (pid) { return api().Restore(S.root, id, pid); },
  });
};

actions.cleanup = function () {
  planAndRun({
    title: t('gui.bk.cleanup'), confirmLabel: t('gui.bk.cleanup'), danger: true,
    plan: function () { return api().PlanCleanup(S.root); },
    run: async function (pid) { return { removed: await api().ApplyCleanup(S.root, pid) }; },
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
    return '<label class="tog"><input type="checkbox" data-bind="m.un.runtimes.' + esc(i.name) + '" data-k="un-' + esc(i.name) + '"' + attr('checked', true, !!u.runtimes[i.name]) + '><b>' + esc(i.label) + '</b><span>' + esc(t('gui.un.runtime.desc')) + '</span></label>';
  }).join('');
  if (!rows) rows = '<div class="tog"><span>' + esc(t('gui.un.no-runtimes')) + '</span></div>';
  var hasProgram = !!(d && d.supported && d.installed);
  return head('uninstall') + busyLine() + '<div class="lbl">' + esc(t('gui.un.what')) + '</div><div class="box">' + rows +
    '<label class="tog' + (all ? '' : ' off') + '"' + attr('title', t('gui.un.guard.needs-all'), !all) + '><input type="checkbox" data-bind="m.un.guard" data-k="un-guard"' + attr('checked', true, !!u.guard && all) + attr('disabled', true, !all) + '><b>' + esc(t('gui.windows.guard')) + '</b><span>' + esc(t('gui.un.guard.desc')) + '</span></label>' +
    '<label class="tog' + (hasProgram ? '' : ' off') + '"><input type="checkbox" data-bind="m.un.program" data-k="un-program"' + attr('checked', true, !!u.program && hasProgram) + attr('disabled', true, !hasProgram) + '><b>' + esc(t('gui.un.program')) + '</b><span>' + esc(t('gui.un.program.desc')) + '</span></label></div>' +
    '<label class="danger"><input type="checkbox" data-bind="m.un.server" data-k="un-server"' + attr('checked', true, !!u.server) + '><span><b>' + esc(t('gui.un.server')) + '</b> ' + esc(t('gui.un.server.warn')) + '</span></label>' +
    '<div class="note"><span aria-hidden="true">ⓘ</span><span>' + esc(t('gui.un.keep')) + ' <a href="#" data-act="go" data-val="backups">' + esc(t('gui.mgr.nav.backups')) + '</a></span></div>' +
    '<div style="display:flex;gap:10px;justify-content:flex-end;margin-top:auto"><button type="button" class="btn" data-act="go" data-val="overview" data-k="un-cancel">' + esc(t('gui.btn.cancel')) + '</button>' +
    '<button type="button" class="btn dng fill" data-act="uninstall" data-k="un-go">' + esc(t('gui.un.go')) + '</button></div>';
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
    root: S.root, runtimes: names.indexOf('-none-') >= 0 ? [] : names, removeMemoryServer: !!u.server, purgeMemoryData: !!u.server, purgeConfirm: '',
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
