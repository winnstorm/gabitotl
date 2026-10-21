'use strict';
// wizard.js: the seven setup screens (global only: no project folder anywhere) (welcome, detect, profile, memory, delivery,
// Windows, install), their validation and the install run. Every visible string
// comes from the message catalogue through t().

var STEPS = [
  ['gui.nav.welcome', stepWelcome],
  ['gui.nav.detect', stepDetect],
  ['gui.nav.profile', stepProfile],
  ['gui.nav.memory', stepMemory],
  ['gui.nav.delivery', stepDelivery],
  ['gui.nav.windows', stepWindows],
  ['gui.nav.install', stepInstall],
];
var STEP_INSTALL = STEPS.length - 1;

var ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
// PROVIDER_ENV is also used by the Manager's per-project delivery form.
var PROVIDER_ENV = { github: 'GITHUB_TOKEN', gitlab: 'GITLAB_TOKEN', bitbucket: 'BITBUCKET_TOKEN' };

function renderWizard() {
  var rail = '<nav class="rail" aria-label="' + esc(t('gui.nav.label')) + '"><ol>' + STEPS.map(function (s, i) {
    var cls = i === S.step ? ' cur' : (i < S.step ? ' done' : '');
    return '<li class="step' + cls + '"' + (i === S.step ? ' aria-current="step"' : '') + '><span class="dot">' + (i < S.step ? '✓' : i + 1) + '</span>' + esc(t(s[0])) + '</li>';
  }).join('') + '</ol><div class="railfoot mono">v' + esc(S.info.version) + ' · ' + esc(S.info.os) + '/' + esc(S.info.arch) + '</div></nav>';
  return titlebar() + '<div class="body">' + rail + '<main class="pane" id="pane">' + STEPS[S.step][1]() + '</main></div>' + footer();
}

function footer() {
  var busy = S.running;
  var left = '<button type="button" class="btn" data-act="cancel"' + attr('disabled', true, busy) + '>' + esc(t('gui.btn.cancel')) + '</button>';
  var alert = S.error ? '<span class="alert" role="alert">' + esc(S.error) + '</span>' : '<span class="sp"></span>';
  var back = S.step > 0 && !(S.finished && !S.installErr) ? '<button type="button" class="btn" data-act="back"' + attr('disabled', true, busy) + '>' + esc(t('gui.btn.back')) + '</button>' : '';
  var next;
  if (S.step < STEP_INSTALL) {
    next = '<button type="button" class="btn pri" data-act="next">' + esc(t('gui.btn.next')) + '</button>';
  } else if (S.finished && !S.installErr) {
    next = '<button type="button" class="btn pri" data-act="finish">' + esc(t('gui.btn.finish')) + '</button>';
  } else if (S.finished) {
    next = '<button type="button" class="btn pri" data-act="replan">' + esc(t('gui.btn.retry')) + '</button>';
  } else {
    next = '<button type="button" class="btn pri" data-act="install"' + attr('disabled', true, busy || !S.plan) + '>' + esc(t('gui.btn.install')) + '</button>';
  }
  if (S.finished && !S.installErr) left = '';
  return '<div class="ft">' + left + alert + back + next + '</div>';
}

// ---- screens ----------------------------------------------------------------

function stepWelcome() {
  return '<div class="hrow" style="gap:18px;margin-top:8px">' + logo(64) + '<div><h1>' + esc(t('gui.welcome.title')) + '</h1><p class="lead">' + esc(t('gui.welcome.tagline')) + '</p></div></div>' +
    '<p class="lead">' + esc(t('gui.welcome.lead')) + '</p>' +
    '<div class="lbl">' + esc(t('gui.welcome.language')) + '</div>' +
    '<div class="grid2">' + [['es', 'gui.welcome.lang.es.name', 'gui.welcome.lang.es.desc'], ['en', 'gui.welcome.lang.en.name', 'gui.welcome.lang.en.desc']].map(function (o) {
      return '<label class="opt' + (S.lang === o[0] ? ' sel' : '') + '"><input type="radio" name="lang" data-act="lang" data-val="' + o[0] + '" data-k="welcome-lang-' + o[0] + '"' + attr('checked', true, S.lang === o[0]) + '><div><b>' + esc(t(o[1])) + '</b><span>' + esc(t(o[2])) + '</span></div></label>';
    }).join('') + '</div>' +
    '<p class="lead sm">' + esc(t('gui.welcome.lang.note')) + '</p>' +
    '<div style="margin-top:auto" class="chk"><input type="checkbox" id="lic" data-bind="a.license" data-k="a.license"' + attr('checked', true, S.a.license) + '><label for="lic">' + esc(t('gui.welcome.license')) + '</label> <a href="#" data-act="license">' + esc(t('gui.welcome.license.link')) + '</a></div>';
}

function runtimeBadge(state) {
  if (state === 'configured' || state === 'installed') return badge('ok', t('gui.detect.badge.found'));
  if (state === 'detected-history') return badge('warn', t('gui.detect.badge.history'));
  return badge('todo', t('gui.detect.badge.missing'));
}

function stepDetect() {
  var head = '<div class="hrow"><div class="grow"><h1>' + esc(t('gui.detect.title')) + '</h1><p class="lead">' + esc(t('gui.detect.lead')) + '</p></div>' +
    '<button type="button" class="btn sm" data-act="scan"' + attr('disabled', true, S.scanning) + '>' + esc(t('gui.btn.scan')) + '</button></div>';
  var d = S.detect;
  if (S.detectErr) return head + '<p class="lead" role="alert">' + esc(S.detectErr) + '</p>';
  if (!d) return head + '<p class="lead">' + esc(t('gui.detect.scanning')) + '</p>';
  var none = t('gui.detect.none');
  var rows = [];
  rows.push(row(t('gui.detect.git'), d.git.found ? (d.git.path || 'git') + (d.userName ? ' · ' + d.userName : '') : none, d.git.found ? badge('ok', t('gui.detect.badge.ready')) : badge('err', t('gui.detect.badge.missing'))));
  d.runtimes.forEach(function (r) { rows.push(row(r.label, r.name, runtimeBadge(r.state))); });
  rows.push(row(t('gui.detect.engram'), d.engram.present ? String(d.engram.observations) : none, d.engram.present ? badge('ok', t('gui.detect.badge.found')) : badge('todo', t('gui.detect.badge.optional'))));
  rows.push(row(t('gui.detect.container'), d.containerEngine || none, d.containerEngine ? badge('ok', t('gui.detect.badge.found')) : badge('todo', t('gui.detect.badge.optional'))));
  rows.push(row(t('gui.detect.pkg'), d.packageManager || none, d.packageManager ? badge('ok', t('gui.detect.badge.ready')) : badge('todo', t('gui.detect.badge.optional'))));
  var out = head + '<div class="box">' + rows.join('') + '</div>';
  if (d.gentleAI.found) {
    out += '<div class="call" role="group" aria-label="Gentle AI"><b>' + esc(t('gui.detect.gentle')) + '</b><div class="opts">' +
      [['migrate', 'gui.detect.gentle.migrate'], ['keep', 'gui.detect.gentle.keep']].map(function (o) {
        return '<label class="chk"><input type="radio" name="g" value="' + o[0] + '" data-bind="a.gentleAction" data-k="gentle-' + o[0] + '"' + attr('checked', true, S.a.gentleAction === o[0]) + '>' + esc(t(o[1])) + '</label>';
      }).join('') + '</div></div>';
  }
  return out;
}

function row(label, value, badgeHTML) {
  return '<div class="row"><b>' + esc(label) + '</b><span class="mono">' + esc(value) + '</span><span class="sp"></span>' + badgeHTML + '</div>';
}

function stepProfile() {
  var d = S.detect || { runtimes: [], gentleAI: {} };
  var rts = d.runtimes.length ? '<div class="grid3">' + d.runtimes.map(function (r) {
    var on = !!S.a.runtimes[r.name];
    return '<div class="rt' + (on ? ' sel' : '') + '"><label><input type="checkbox" data-bind="a.runtimes.' + esc(r.name) + '" data-k="rt-' + esc(r.name) + '"' + attr('checked', true, on) + '>' + esc(r.label) + '</label>' +
      '<select aria-label="' + esc(r.label + ' — ' + t('gui.profile.integrate')) + '" title="' + esc(t('gui.profile.scope.project.hint')) + '"><option selected>' + esc(t('gui.profile.scope.global')) + '</option><option disabled>' + esc(t('gui.profile.scope.project')) + '</option></select></div>';
  }).join('') + '</div><p class="hint">' + esc(t('gui.profile.scope.project.hint')) + '</p>' : '<p class="lead">' + esc(t('gui.profile.noruntimes')) + '</p>';
  var noGentle = !d.gentleAI.found;
  return '<div><h1>' + esc(t('gui.profile.title')) + '</h1><p class="lead">' + esc(t('gui.profile.lead')) + '</p></div>' +
    '<div class="grid2">' + field('a.name', t('gui.profile.name'), { hint: t('gui.profile.name.hint') }) +
    '<div class="field">' + esc(t('gui.profile.mode')) + seg('a.personaMode', [['lite', 'gui.profile.mode.lite'], ['normal', 'gui.profile.mode.normal'], ['full', 'gui.profile.mode.full']], t('gui.profile.mode')) + '<span class="hint">' + esc(t('gui.profile.mode.hint')) + '</span></div></div>' +
    '<div class="lbl">' + esc(t('gui.profile.integrate')) + '</div>' + rts +
    '<div style="display:flex;flex-direction:column;gap:10px;margin-top:4px">' +
    check('a.migratePersona', t('gui.profile.migrate'), { disabled: noGentle, title: noGentle ? t('note.gentle-ai-not-found') : '' }) +
    check('a.retireGentleKiro', t('gui.profile.retire-kiro'), { disabled: !d.gentleAI.gentleKiroPresent, title: d.gentleAI.gentleKiroPresent ? '' : t('note.gentle-kiro-none') }) + '</div>';
}

function stepMemory() {
  var m = S.a.memory;
  var team = m.mode === 'team';
  var out = '<div><h1>' + esc(t('gui.memory.title')) + '</h1><p class="lead">' + esc(t('gui.memory.lead')) + '</p></div>' +
    '<div class="grid2">' + radio('a.memory.mode', 'solo', t('gui.memory.solo'), t('gui.memory.solo.desc')) + radio('a.memory.mode', 'team', t('gui.memory.team'), t('gui.memory.team.desc')) + '</div>';
  if (!team) return out;
  var canImport = S.detect && S.detect.engram.canImport;
  out += '<div class="panel"><div class="field">' + esc(t('gui.memory.role')) + seg('a.memory.role', [['host', 'gui.memory.role.host'], ['client', 'gui.memory.role.client']], t('gui.memory.role')) + '</div>';
  if (m.role === 'host') {
    out += '<div class="grid3"><label class="field">' + esc(t('gui.memory.engine')) + '<select data-bind="a.memory.engine" data-k="a.memory.engine">' +
      [['auto', 'gui.memory.engine.auto'], ['docker', 'gui.memory.engine.docker'], ['podman', 'gui.memory.engine.podman']].map(function (o) {
        return '<option value="' + o[0] + '"' + attr('selected', true, m.engine === o[0]) + '>' + esc(t(o[1])) + '</option>';
      }).join('') + '</select></label>' +
      field('a.memory.port', t('gui.memory.port'), { mono: true, placeholder: t('gui.memory.port.auto') }) +
      field('a.memory.instance', t('gui.memory.instance'), { placeholder: 'default' }) + '</div>' +
      field('a.memory.url', t('gui.memory.url'), { mono: true, hint: t('gui.memory.url.hint') });
  } else {
    out += field('a.memory.url', t('gui.memory.url.client'), { mono: true }) +
      field('a.memory.tokenEnv', t('gui.memory.token'), { mono: true, placeholder: 'MEMORY_TOKEN', hint: t('gui.memory.token.hint') });
  }
  out += check('a.memory.importEngram', t('gui.memory.import'), { disabled: !canImport, title: canImport ? '' : t('gui.memory.import.none') }) + '</div>';
  return out;
}

var CRED_PLATFORMS = [['github', 'gui.delivery.platform.github'], ['gitlab', 'gui.delivery.platform.gitlab'], ['bitbucket', 'gui.delivery.platform.bitbucket']];
var CRED_STATUS = { saved: 'gui.delivery.cred.saved', rejected: 'gui.delivery.cred.rejected', unreachable: 'gui.delivery.cred.unreachable', invalid: 'gui.delivery.cred.invalid' };

function stepDelivery() {
  return '<div><h1>' + esc(t('gui.delivery.title')) + '</h1><p class="lead">' + esc(t('gui.delivery.lead')) + '</p></div>' +
    deliveryForm('a.delivery') + credentialsBlock();
}

// deliveryForm is the automation, merge method, draft and branch cleanup fields
// bound to the state object at path (the wizard, the Manager's global defaults
// and a project's override share it).
function deliveryForm(base) {
  var dl = get(base);
  var all = dl.automation === 'all';
  return '<div class="field">' + esc(t('gui.delivery.automation')) + '<div class="grid3">' +
    radio(base + '.automation', 'all', t('gui.delivery.auto.all'), t('gui.delivery.auto.all.desc')) +
    radio(base + '.automation', 'until-pr', t('gui.delivery.auto.until-pr'), t('gui.delivery.auto.until-pr.desc')) +
    radio(base + '.automation', 'ask', t('gui.delivery.auto.ask'), t('gui.delivery.auto.ask.desc')) + '</div></div>' +
    '<div class="grid2" style="align-items:end"><label class="field' + (all ? '' : ' off') + '" style="' + (all ? '' : 'color:var(--muted)') + '">' + esc(t('gui.delivery.merge')) +
    '<select data-bind="' + base + '.mergeMethod" data-k="' + base + '.mergeMethod"' + attr('disabled', true, !all) + '>' +
    [['squash', 'gui.delivery.merge.squash'], ['merge', 'gui.delivery.merge.merge'], ['rebase', 'gui.delivery.merge.rebase']].map(function (o) {
      return '<option value="' + o[0] + '"' + attr('selected', true, dl.mergeMethod === o[0]) + '>' + esc(t(o[1])) + '</option>';
    }).join('') + '</select><span class="hint">' + esc(t('gui.delivery.merge.hint')) + '</span></label>' +
    '<div style="display:flex;flex-direction:column;gap:10px;padding-bottom:20px">' + check(base + '.draft', t('gui.delivery.draft')) +
    check(base + '.deleteBranch', t('gui.delivery.delete-branch'), { disabled: !all, title: all ? '' : t('gui.delivery.merge.hint') }) + '</div></div>';
}

// credResultMsg turns a Save/Test result into the line shown next to a row: a
// success names the account when the platform told, any other outcome shows the
// secret-free detail (certificate problems included) or a generic status text.
var CRED_CODES = {
  cert_untrusted: 'gui.cred.err.cert_untrusted', rejected: 'gui.cred.err.rejected', no_permission: 'gui.cred.err.no_permission',
  unreachable: 'gui.cred.err.unreachable', timeout: 'gui.cred.err.timeout', insecure_http: 'gui.cred.err.insecure_http',
  host_conflict: 'gui.cred.err.host_conflict', invalid_url: 'gui.cred.err.invalid_url', not_stored: 'gui.cred.err.not_stored',
  unknown_host: 'gui.cred.err.unknown_host'
};

function credResultMsg(r) {
  var ok = r.status === 'saved' || r.status === 'ok';
  if (ok) return { cls: 'ok', text: r.user ? t('gui.delivery.cred.ok.user', { user: r.user }) : t(r.status === 'saved' ? 'gui.delivery.cred.saved' : 'gui.delivery.cred.ok') };
  // A known failure code is worded by the catalogue (in the language on screen); the
  // backend's English detail is only the fallback for a code the page does not know.
  if (r.code && CRED_CODES[r.code]) return { cls: 'warn', text: t(CRED_CODES[r.code]) };
  return { cls: 'warn', text: r.detail || t(CRED_STATUS[r.status] || CRED_STATUS.invalid) };
}

function credMsgHTML(key) {
  var msg = S.cred.msg[key];
  return msg ? '<span class="badge ' + esc(msg.cls) + '" role="status">' + esc(msg.text) + '</span>' : '';
}

function credBtn(act, label, key, extra) {
  return '<button type="button" class="btn sm" data-act="' + act + '" data-k="' + act + '-' + esc(key) + '" ' + extra + attr('disabled', true, !!S.cred.busy) + '>' + esc(label) + '</button>';
}

// credentialsBlock is one row per platform: stored or not, a write-only token
// field and its outcome, then the self-hosted servers. The token is never read
// back from Go.
function credentialsBlock() {
  var c = S.cred;
  var head = '<div class="lbl">' + esc(t('gui.delivery.creds.title')) + '</div><p class="hint">' + esc(t('gui.delivery.creds.lead')) + '</p>';
  if (c.err) return head + '<p class="hint" role="alert">' + esc(c.err) + '</p>';
  return head + '<div class="box">' + CRED_PLATFORMS.map(function (p) {
    var info = (c.list || []).filter(function (x) { return x.platform === p[0]; })[0] || { host: '', stored: false };
    var busy = c.busy === p[0];
    var label = t(p[1]) + ' \u2014 ' + t('gui.delivery.cred.input');
    return '<div class="row"><b>' + esc(t(p[1])) + '</b><span class="mono">' + esc(info.host) + '</span><span class="sp"></span>' +
      badge(info.stored ? 'ok' : 'todo', info.stored ? t('gui.delivery.cred.stored') : t('gui.delivery.cred.none')) +
      (info.stored ? credBtn('credtest', t('gui.delivery.cred.test'), p[0], 'data-cred="' + esc(p[0]) + '" data-host="' + esc(info.host) + '" ') : '') +
      (info.stored ? credBtn('credremove', t('gui.delivery.cred.remove'), p[0], 'data-cred="' + esc(p[0]) + '" data-host="' + esc(info.host) + '" ') : '') + '</div>' +
      '<div class="row"><input class="mono" style="flex:1;height:34px;border:1px solid var(--line2);border-radius:8px;padding:0 12px" type="password" autocomplete="off" spellcheck="false" aria-label="' + esc(label) + '" data-bind="cred.tok.' + esc(p[0]) + '" data-cred="' + esc(p[0]) + '" data-k="cred-' + esc(p[0]) + '" data-norender value="' + esc(c.tok[p[0]]) + '"' + attr('disabled', true, !!c.busy) + '>' +
      '<button type="button" class="btn sm" data-act="credsave" data-cred="' + esc(p[0]) + '" data-k="credsave-' + esc(p[0]) + '"' + attr('disabled', true, !!c.busy) + '>' + esc(busy ? t('gui.delivery.cred.working') : t('gui.delivery.cred.save')) + '</button>' + credMsgHTML(p[0]) + '</div>';
  }).join('') + '</div>' + serversBlock();
}

var SRV_PLATFORMS = [['bitbucket-server', 'gui.delivery.platform.bitbucket-server'], ['github', 'gui.delivery.platform.github-enterprise'], ['gitlab', 'gui.delivery.platform.gitlab-self']];

function srvPlatformLabel(p) {
  var m = SRV_PLATFORMS.filter(function (x) { return x[0] === p; })[0];
  return m ? t(m[1]) : p;
}

// serversBlock lists the registered self-hosted servers and the form that adds one.
function serversBlock() {
  var c = S.cred, f = c.srv;
  var out = '<div class="lbl">' + esc(t('gui.servers.title')) + '</div><p class="hint">' + esc(t('gui.servers.lead')) + '</p><div class="box">';
  var list = c.servers || [];
  if (!list.length) out += '<div class="row"><span class="hint">' + esc(t('gui.servers.none')) + '</span></div>';
  list.forEach(function (s) {
    var key = 'srv:' + s.host;
    var d = 'data-cred="' + esc(s.platform) + '" data-host="' + esc(s.host) + '" ';
    out += '<div class="row"><b>' + esc(s.host) + '</b><span>' + esc(srvPlatformLabel(s.platform)) + '</span><span class="mono">' + esc(s.url) + '</span><span class="sp"></span>' +
      badge(s.stored ? 'ok' : 'todo', s.stored ? t('gui.delivery.cred.stored') : t('gui.delivery.cred.none')) +
      (s.stored ? credBtn('credtest', t('gui.delivery.cred.test'), key, d) : '') + credBtn('srvremove', t('gui.delivery.cred.remove'), key, d) + credMsgHTML(key) + '</div>';
  });
  out += '<div class="row"><label class="field" style="margin:0">' + esc(t('gui.servers.platform')) +
    '<select data-bind="cred.srv.platform" data-k="srv-platform"' + attr('disabled', true, !!c.busy) + '>' + SRV_PLATFORMS.map(function (o) {
      return '<option value="' + o[0] + '"' + attr('selected', true, f.platform === o[0]) + '>' + esc(t(o[1])) + '</option>';
    }).join('') + '</select></label>' +
    '<label class="field" style="margin:0;flex:1">' + esc(t('gui.servers.url')) +
    '<input class="mono" type="text" autocomplete="off" spellcheck="false" placeholder="https://bitbucket.example.com:8443" data-bind="cred.srv.url" data-k="srv-url" data-norender value="' + esc(f.url) + '"' + attr('disabled', true, !!c.busy) + '></label></div>' +
    '<div class="row"><label class="field" style="margin:0;flex:1">' + esc(t('gui.servers.ssh')) +
    '<input class="mono" type="text" autocomplete="off" spellcheck="false" placeholder="ssh.example.com" data-bind="cred.srv.ssh" data-k="srv-ssh" data-norender value="' + esc(f.ssh) + '"' + attr('disabled', true, !!c.busy) + '>' +
    '<span class="hint">' + esc(t('gui.servers.ssh.hint')) + '</span></label></div>' +
    '<div class="row"><input class="mono" style="flex:1;height:34px;border:1px solid var(--line2);border-radius:8px;padding:0 12px" type="password" autocomplete="off" spellcheck="false" aria-label="' + esc(t('gui.servers.token')) + '" placeholder="' + esc(t('gui.servers.token')) + '" data-bind="cred.srv.tok" data-srvadd="1" data-k="srv-tok" data-norender value="' + esc(f.tok) + '"' + attr('disabled', true, !!c.busy) + '>' +
    '<button type="button" class="btn sm" data-act="srvsave" data-k="srv-save"' + attr('disabled', true, !!c.busy) + '>' + esc(c.busy === 'srv:new' ? t('gui.delivery.cred.working') : t('gui.delivery.cred.save')) + '</button>' + credMsgHTML('srv:new') + '</div>' +
    (f.conflict ? '<div class="row"><span class="hint" role="alert">' + esc(t('gui.servers.conflict')) + '</span><span class="sp"></span>' +
      '<button type="button" class="btn sm" data-act="srvreplace" data-k="srv-replace"' + attr('disabled', true, !!c.busy) + '>' + esc(t('gui.servers.replace')) + '</button></div>' : '') + '</div>';
  return out;
}

function codegraphLines() {
  var g = S.cg;
  if (!g) return { text: t('gui.windows.codegraph.checking'), shadow: '' };
  var text;
  if (!g.supported) text = t('gui.windows.codegraph.unsupported') + (g.hint ? ' ' + g.hint : '');
  else if (g.action === 'install') text = t('gui.windows.codegraph.install', { version: g.pinned });
  else if (g.action === 'upgrade') text = t('gui.windows.codegraph.upgrade', { installed: g.version, version: g.pinned });
  else if (g.pathOnly) text = t('gui.windows.codegraph.path', { version: g.version });
  else text = t('gui.windows.codegraph.ok', { version: g.version });
  return { text: text, shadow: g.shadow ? t('gui.windows.codegraph.shadow', { path: g.shadow }) : '' };
}

function stepWindows() {
  var supported = !!S.info.installDir;
  var toggle = function (path, title, desc, o) {
    o = o || {};
    return '<label class="tog' + (o.disabled ? ' off' : '') + '"><input type="checkbox" data-bind="' + path + '" data-k="' + path + '"' + attr('checked', true, !!get(path)) + attr('disabled', true, !!o.disabled) + '><b>' + esc(title) + '</b><span>' + esc(desc) + '</span></label>';
  };
  var cg = codegraphLines();
  return '<div><h1>' + esc(t('gui.windows.title')) + '</h1><p class="lead">' + esc(supported ? t('gui.windows.lead') : t('gui.windows.unsupported')) + '</p></div>' +
    '<div class="hrow">' + field('a.win.installDir', t('gui.windows.dir'), { mono: true, style: 'flex:1', disabled: !supported }) +
    '<button type="button" class="btn sm" data-act="browse"' + attr('disabled', true, !supported) + '>' + esc(t('gui.btn.browse')) + '</button></div>' +
    '<div class="lbl">' + esc(t('gui.windows.shortcuts')) + '</div><div class="box">' +
    toggle('a.win.startMenu', t('gui.windows.startmenu'), t('gui.windows.startmenu.desc'), { disabled: !supported }) +
    toggle('a.win.desktopShortcut', t('gui.windows.desktop'), t('gui.windows.desktop.desc'), { disabled: !supported }) +
    toggle('a.win.addToPath', t('gui.windows.path'), t('gui.windows.path.desc'), { disabled: !supported }) + '</div>' +
    '<div class="lbl">' + esc(t('gui.windows.required')) + '</div><div class="box">' +
    '<label class="tog off"><input type="checkbox" checked disabled aria-describedby="cg-status"><b>' + esc(t('gui.windows.codegraph')) + '</b><span>' + esc(t('gui.windows.codegraph.desc')) + '</span></label>' +
    '<div class="tog" id="cg-status" role="status"><span>' + esc(cg.text) + (cg.shadow ? ' ' + esc(cg.shadow) : '') + '</span></div></div>';
}

function selectedRuntimes() {
  return ((S.detect && S.detect.runtimes) || []).filter(function (r) { return S.a.runtimes[r.name]; });
}

function stepInstall() {
  var title = S.finished ? (S.installErr ? t('gui.install.failed') : t('gui.install.done')) : (S.running ? t('gui.install.running') : t('gui.install.review'));
  var lead = S.finished && !S.installErr ? t('gui.install.done.lead') : t('gui.install.lead');
  var out = '<div><h1>' + esc(S.running ? t('gui.install.title') : title) + '</h1><p class="lead">' + esc(lead) + '</p></div>';
  if (S.planErr) return out + '<p class="lead" role="alert">' + esc(S.planErr) + '</p>';
  if (!S.plan) return out + '<p class="lead">' + esc(t('gui.install.planning')) + '</p>';
  var m = S.a.memory;
  var memText = m.mode === 'solo' ? t('gui.install.sum.memory.solo') : t(m.role === 'host' ? 'gui.install.sum.memory.host' : 'gui.install.sum.memory.client');
  var dl = S.a.delivery;
  var autoKey = { all: 'gui.delivery.auto.all', 'until-pr': 'gui.delivery.auto.until-pr', ask: 'gui.delivery.auto.ask' };
  var delivery = [t(autoKey[dl.automation]), t('gui.install.sum.delivery.global')];
  if (dl.draft) delivery.push(t('gui.delivery.draft'));
  var agents = selectedRuntimes().map(function (r) { return r.label; });
  out += '<dl class="sum">' +
    '<dt>' + esc(t('gui.install.sum.agents')) + '</dt><dd>' + esc(agents.length ? agents.join(', ') + ' (' + t('gui.install.sum.global') + ')' : t('gui.install.sum.none')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.memory')) + '</dt><dd>' + esc(memText) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.delivery')) + '</dt><dd>' + esc(delivery.join(' · ')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.creds')) + '</dt><dd>' + esc(t('gui.install.sum.creds.n', { n: storedCount() })) + '</dd>' +
    (S.cg && S.cg.supported ? '<dt>' + esc(t('gui.install.sum.codegraph')) + '</dt><dd>' + esc(t('gui.install.sum.codegraph.value', { version: S.cg.pinned })) + '</dd>' : '') +
    '<dt>' + esc(t('gui.install.sum.windows')) + '</dt><dd class="mono">' + esc(S.a.win.installDir || t('gui.install.sum.none')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.backup')) + '</dt><dd>' + esc(S.plan.snapshot ? S.plan.snapshot : t('gui.install.sum.backup.value')) + '</dd></dl>';
  if (!S.running && !S.finished) return out + planDetails(S.plan);
  out += '<div style="display:flex;flex-direction:column;gap:8px"><div style="display:flex;font-size:13px"><b style="flex:1">' + esc(S.progressMsg) + '</b><span class="mono">' + S.pct + '%</span></div>' +
    '<div class="bar" role="progressbar" aria-label="' + esc(t('gui.install.progress')) + '" aria-valuenow="' + S.pct + '" aria-valuemin="0" aria-valuemax="100"><div style="width:' + S.pct + '%"></div></div></div>' +
    '<div class="log mono" role="log" aria-label="' + esc(t('gui.install.log')) + '">' + S.log.map(logLine).join('') + '</div>';
  if (S.finished) out += resultBlock();
  return out;
}

function storedCount() {
  return (S.cred.list || []).filter(function (c) { return c.stored; }).length;
}

function planDetails(p) {
  var html = '';
  (p.groups || []).forEach(function (g) {
    html += '<h3>' + esc(g.label) + '</h3><ul>' + (g.lines || []).map(function (l) { return '<li>' + esc(l) + '</li>'; }).join('') + '</ul>';
  });
  if ((p.steps || []).length) html += '<h3>' + esc(t('gui.install.steps')) + '</h3><ul>' + p.steps.map(function (s) { return '<li>' + esc(s.title) + '</li>'; }).join('') + '</ul>';
  (p.notes || []).forEach(function (n) { html += '<p class="hint">' + esc(n.text) + '</p>'; });
  return html ? '<div class="plan" tabindex="0" aria-label="' + esc(t('gui.install.changes')) + '">' + html + '</div>' : '';
}

function logLine(e) {
  var sym = { running: ['r', '›'], done: ['k', '✓'], warn: ['w', '!'], error: ['e', '×'] }[e.status] || ['r', '›'];
  return '<div><span class="' + sym[0] + '">' + sym[1] + '</span> ' + esc(e.message) + '</div>';
}

function resultBlock() {
  var r = S.result;
  var html = '';
  if (S.installErr) html += '<p class="alert" style="text-align:left" role="alert">' + esc(S.installErr) + '</p>' + (r && r.rolledBack ? '<p class="lead">' + esc(t('gui.install.rolledback')) + '</p>' : '');
  if (r) {
    var bad = (r.steps || []).filter(function (s) { return s.status === 'error'; });
    var badChecks = (r.checks || []).filter(function (c) { return !c.ok; });
    if (badChecks.length) html += '<ul>' + badChecks.map(function (c) { return '<li class="mono">' + esc(c.group + ' ' + c.name + (c.detail ? ': ' + c.detail : '')) + '</li>'; }).join('') + '</ul>';
    if (bad.length) html += '<p class="lead">' + esc(t('gui.install.step.warn')) + '</p><ul>' + bad.map(function (s) { return '<li class="mono">' + esc(s.id + ': ' + s.message) + '</li>'; }).join('') + '</ul>';
    if ((r.tokens || []).length) html += '<p class="lbl">' + esc(t('gui.install.tokens')) + '</p><ul>' + r.tokens.map(function (k) { return '<li class="mono">' + esc(k.user + ': ' + k.token) + '</li>'; }).join('') + '</ul>';
  }
  return html;
}

// ---- navigation & validation --------------------------------------------------

function validate(step) {
  var a = S.a;
  switch (step) {
    case 0: return a.license ? '' : t('gui.welcome.license.required');
    case 1:
      if (S.detect && S.detect.gentleAI.found && a.gentleAction !== 'migrate' && a.gentleAction !== 'keep') return t('gui.detect.gentle.required');
      return S.detect ? '' : t('gui.detect.scanning');
    case 2: return a.name.trim() ? '' : t('gui.profile.name.required');
    case 3:
      if (a.memory.mode === 'team' && a.memory.role === 'client' && !a.memory.url.trim()) return t('gui.memory.url.required');
      if (a.memory.mode === 'team' && a.memory.role === 'client' && a.memory.tokenEnv && !ENV_NAME.test(a.memory.tokenEnv)) return t('gui.memory.token.invalid');
      return '';
    case 4: return '';
    case 5: return S.info.installDir && !a.win.installDir.trim() ? t('gui.windows.dir.required') : '';
  }
  return '';
}

actions.license = function (el, ev) {
  ev.preventDefault();
  if (window.runtime && window.runtime.BrowserOpenURL) api().OpenLicense();
};

actions.cancel = function () {
  if (window.confirm(t('gui.confirm.cancel'))) api().Quit();
};

actions.back = function () {
  S.error = '';
  S.step = Math.max(0, S.step - 1);
  render();
};

actions.next = async function () {
  S.error = validate(S.step);
  if (S.error) return render();
  S.step++;
  if (S.step === 1 && !S.detect) scan();
  if (S.step === 2) applyDetectDefaults();
  if (S.step === 4) loadDelivery();
  if (S.step === 5) loadCodeGraph();
  if (S.step === STEP_INSTALL) replan();
  render();
};

// applyDetectDefaults fills the answers the scan can suggest, once.
var defaultsApplied = false;
function applyDetectDefaults() {
  var d = S.detect;
  if (!d || defaultsApplied) return;
  defaultsApplied = true;
  if (!S.a.name) S.a.name = d.userName || '';
  if (d.personaMode) S.a.personaMode = d.personaMode;
  d.runtimes.forEach(function (r) { S.a.runtimes[r.name] = r.usable; });
  if (!d.gentleAI.found) S.a.gentleAction = '';
}

async function scan() {
  S.scanning = true;
  S.detectErr = '';
  render();
  try {
    S.detect = await api().Detect('');
    defaultsApplied = false;
  } catch (e) {
    S.detectErr = t('gui.error.generic', { detail: errText(e) });
  }
  S.scanning = false;
  render();
}

actions.scan = scan;

actions.browse = async function () {
  try {
    var dir = await api().ChooseFolder(t('gui.windows.dir'), S.a.win.installDir);
    if (dir) {
      S.a.win.installDir = dir;
      render();
    }
  } catch (e) {
    S.error = t('gui.error.generic', { detail: errText(e) });
    render();
  }
};

// loadDelivery fills the delivery step: the saved global defaults (once) and
// which platforms already have a token.
var deliveryLoaded = false;
async function loadDelivery() {
  await loadCreds();
  if (deliveryLoaded) return;
  deliveryLoaded = true;
  try {
    var g = await api().GlobalDelivery();
    if (g && g.saved) {
      S.a.delivery.automation = g.automation === 'custom' ? S.a.delivery.automation : g.automation;
      S.a.delivery.mergeMethod = g.mergeMethod || S.a.delivery.mergeMethod;
      S.a.delivery.deleteBranch = !!g.deleteBranch;
      S.a.delivery.draft = !!g.draft;
    }
  } catch (e) { /* the built-in defaults stay */ }
  render();
}

async function loadCreds() {
  try {
    S.cred.list = await api().Credentials();
    S.cred.err = '';
    await loadServersQuiet();
  } catch (e) {
    S.cred.err = t('gui.delivery.cred.unavailable');
  }
  render();
}

async function loadServersQuiet() {
  try { S.cred.servers = await api().Servers(); } catch (e) { S.cred.servers = []; }
}

async function loadCodeGraph() {
  try { S.cg = await api().CodeGraph(); } catch (e) { S.cg = null; }
  render();
}

actions.credsave = async function (el) {
  var p = el.getAttribute('data-cred');
  var tok = (S.cred.tok[p] || '').trim();
  if (!tok || S.cred.busy) {
    S.cred.msg[p] = { cls: 'warn', text: t('gui.delivery.cred.invalid') };
    return render();
  }
  // The token leaves the page state on every attempt, saved or not; a failed
  // one is typed again.
  S.cred.tok[p] = '';
  S.cred.busy = p;
  S.cred.msg[p] = null;
  render();
  try {
    var r = await api().SaveCredential(p, '', tok);
    S.cred.msg[p] = credResultMsg(r);
  } catch (e) {
    S.cred.msg[p] = { cls: 'warn', text: t('gui.error.generic', { detail: errText(e) }) };
  }
  S.cred.busy = '';
  await loadCreds();
};

actions.credremove = async function (el) {
  var p = el.getAttribute('data-cred');
  S.cred.busy = p;
  render();
  try {
    await api().DeleteCredential(el.getAttribute('data-host'));
    S.cred.msg[p] = { cls: 'todo', text: t('gui.delivery.cred.removed') };
  } catch (e) {
    S.cred.msg[p] = { cls: 'warn', text: t('gui.error.generic', { detail: errText(e) }) };
  }
  S.cred.busy = '';
  await loadCreds();
};

// credtest asks the platform whether the stored token is still accepted.
actions.credtest = async function (el) {
  var p = el.getAttribute('data-cred'), host = el.getAttribute('data-host');
  var key = el.getAttribute('data-key') || host && S.cred.servers && S.cred.servers.some(function (s) { return s.host === host; }) ? 'srv:' + host : p;
  if (S.cred.busy) return;
  S.cred.busy = key;
  S.cred.msg[key] = null;
  render();
  try {
    S.cred.msg[key] = credResultMsg(await api().TestCredential(p, host));
  } catch (e) {
    S.cred.msg[key] = { cls: 'warn', text: t('gui.error.generic', { detail: errText(e) }) };
  }
  S.cred.busy = '';
  render();
};

// saveServer verifies a token against the server and registers it only when
// accepted. A host name that already belongs to another server answers a conflict;
// the person confirms with "Replace" and types the token again (it never stays in
// the page state, so a replacement is always a fresh paste).
async function saveServer(replace) {
  var f = S.cred.srv, tok = (f.tok || '').trim(), url = (f.url || '').trim();
  if (S.cred.busy) return;
  if (!url || !tok) {
    S.cred.msg['srv:new'] = { cls: 'warn', text: t(replace && url ? 'gui.servers.retoken' : 'gui.servers.missing') };
    return render();
  }
  var ssh = (f.ssh || '').split(/[\s,]+/).filter(Boolean);
  // The token leaves the page state on every attempt, saved or not.
  f.tok = '';
  f.conflict = false;
  S.cred.busy = 'srv:new';
  S.cred.msg['srv:new'] = null;
  render();
  try {
    var r = await api().SaveServer(f.platform, url, tok, ssh, !!replace);
    S.cred.msg['srv:new'] = credResultMsg(r);
    if (r.status === 'conflict') f.conflict = true;
    if (r.status === 'saved') { f.url = ''; f.ssh = ''; }
  } catch (e) {
    S.cred.msg['srv:new'] = { cls: 'warn', text: t('gui.error.generic', { detail: errText(e) }) };
  }
  tok = '';
  S.cred.busy = '';
  await loadCreds();
}

actions.srvsave = function () { return saveServer(false); };
actions.srvreplace = function () { return saveServer(true); };

actions.srvremove = async function (el) {
  var host = el.getAttribute('data-host'), key = 'srv:' + host;
  if (S.cred.busy) return;
  S.cred.busy = key;
  render();
  try {
    await api().DeleteServer(host);
    S.cred.msg[key] = null;
    S.cred.msg['srv:new'] = { cls: 'todo', text: t('gui.servers.removed', { host: host }) };
  } catch (e) {
    S.cred.msg[key] = { cls: 'warn', text: t('gui.error.generic', { detail: errText(e) }) };
  }
  S.cred.busy = '';
  await loadCreds();
};

// Enter in a token field saves it.
document.addEventListener('keydown', function (ev) {
  if (ev.key === 'Enter' && ev.target.getAttribute && ev.target.tagName === 'INPUT' && ev.target.getAttribute('data-srvadd')) {
    ev.preventDefault();
    return actions.srvsave(ev.target);
  }
  var p = ev.key === 'Enter' && ev.target.getAttribute && ev.target.getAttribute('data-cred');
  if (p && ev.target.tagName === 'INPUT') {
    ev.preventDefault();
    actions.credsave(ev.target);
  }
});

// buildAnswers turns the wizard state into the backend's InstallAnswers.
function buildAnswers() {
  var a = S.a;
  var m = a.memory;
  var team = m.mode === 'team';
  var host = team && m.role === 'host';
  var client = team && m.role === 'client';
  var all = a.delivery.automation === 'all';
  var d = S.detect || { gentleAI: {} };
  return {
    root: '',
    lang: S.lang,
    gentleAction: d.gentleAI.found ? a.gentleAction : '',
    name: a.name.trim(),
    user: '',
    personaMode: a.personaMode,
    runtimes: selectedRuntimes().map(function (r) { return r.name; }),
    scope: 'global',
    migratePersona: !!a.migratePersona && !!d.gentleAI.found,
    retireGentleKiro: !!a.retireGentleKiro && !!d.gentleAI.gentleKiroPresent,
    memory: {
      mode: m.mode, role: team ? m.role : '', engine: host ? m.engine : '',
      port: host ? parseInt(m.port, 10) || 0 : 0, instance: host ? m.instance.trim() : '', url: team ? m.url.trim() : '', tokenEnv: client ? m.tokenEnv.trim() : '',
      storeToken: false, importEngram: m.mode === 'team' && !!m.importEngram, invites: [],
    },
    delivery: {
      provider: '', automation: a.delivery.automation, mergeMethod: all ? a.delivery.mergeMethod : '',
      deleteBranch: all ? !!a.delivery.deleteBranch : null, draft: !!a.delivery.draft, tokenEnv: '',
    },
    windows: {
      installDir: a.win.installDir.trim(), startMenu: !!a.win.startMenu, desktopShortcut: !!a.win.desktopShortcut,
      addToPath: !!a.win.addToPath,
    },
    dryRun: false,
  };
}

async function replan() {
  S.plan = null;
  S.planErr = '';
  S.finished = false;
  S.installErr = '';
  S.result = null;
  S.log = [];
  S.pct = 0;
  render();
  try {
    S.plan = await api().PlanInstall(buildAnswers());
  } catch (e) {
    S.planErr = t('gui.error.generic', { detail: errText(e) });
  }
  render();
}

actions.replan = replan;

function onProgress(ev) {
  if (!ev) return;
  S.pct = Math.max(S.pct, ev.percent || 0);
  S.progressMsg = ev.message || S.progressMsg;
  S.log.push(ev);
  if (S.running) render();
}

actions.install = async function () {
  if (!S.plan || S.running) return;
  S.running = true;
  S.finished = false;
  S.installErr = '';
  S.log = [];
  S.pct = 0;
  S.progressMsg = t('gui.install.running');
  render();
  try {
    S.result = await api().Install(buildAnswers(), S.plan.id);
    if (S.result && S.result.error) S.installErr = t('gui.error.generic', { detail: S.result.error });
    else S.pct = 100;
  } catch (e) {
    S.installErr = t('gui.error.generic', { detail: errText(e) });
  }
  S.running = false;
  S.finished = true;
  render();
};

actions.finish = async function () {
  try {
    await api().Finish(S.a.win.installDir);
  } catch (e) {
    S.error = t('gui.error.generic', { detail: errText(e) });
    render();
  }
};
