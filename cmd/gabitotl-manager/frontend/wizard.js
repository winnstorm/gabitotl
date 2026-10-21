'use strict';
// wizard.js: the seven setup screens (global only: no project folder anywhere) (welcome, detect, profile, memory, delivery,
// Windows, install), their validation and the install run. Every visible string
// comes from the message catalogue through t().

var STEPS = [
  ['gui.nav.welcome', stepWelcome, 'bot'],
  ['gui.nav.detect', stepDetect, 'search'],
  ['gui.nav.profile', stepProfile, 'user'],
  ['gui.nav.memory', stepMemory, 'db'],
  ['gui.nav.delivery', stepDelivery, 'pr'],
  ['gui.nav.windows', stepWindows, 'desktop'],
  ['gui.nav.install', stepInstall, 'download'],
];
var STEP_INSTALL = STEPS.length - 1;

var ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
// PROVIDER_ENV is also used by the Manager's per-project delivery form.
var PROVIDER_ENV = { github: 'GITHUB_TOKEN', gitlab: 'GITLAB_TOKEN', bitbucket: 'BITBUCKET_TOKEN' };

// stepsLocked: the stepper cannot be used while the install runs or after it succeeded.
function stepsLocked() { return S.running || (S.finished && !S.installErr); }

// The stepper: finished steps are buttons that go back (answers are kept), the
// current one is aria-current, the ones ahead are plain text (forward is gated by Next).
function stepper() {
  var n = STEPS.length, locked = stepsLocked();
  var pct = Math.round(S.step / (n - 1) * 100);
  var items = STEPS.map(function (s, i) {
    var label = esc(t(s[0]));
    if (i < S.step) {
      return '<li class="step done"><button type="button" class="stepbtn" data-act="goto" data-val="' + i + '" data-k="step-' + i + '" aria-label="' + esc(t('gui.nav.goto', { step: t(s[0]) })) + '"' + attr('disabled', true, locked) + '><span class="dot">' + UI.icon('check') + '</span>' + label + '</button></li>';
    }
    if (i === S.step) return '<li class="step cur" aria-current="step"><span class="stepbtn"><span class="dot">' + (i + 1) + '</span>' + label + '</span></li>';
    return '<li class="step ahead"><span class="stepbtn"><span class="dot">' + (i + 1) + '</span>' + label + '</span></li>';
  }).join('');
  return '<div class="steps"><div class="rail-line" style="height:' + (n - 1) * 44 + 'px"><div class="rail-fill" data-gid="rail" data-gp="height" data-gv="' + pct + '" style="height:' + pct + '%"></div></div><ol>' + items + '</ol></div>';
}

function renderWizard() {
  var rail = '<nav class="rail" aria-label="' + esc(t('gui.nav.label')) + '">' + stepper() + '<div class="railfoot mono">v' + esc(S.info.version) + ' · ' + esc(S.info.os) + '/' + esc(S.info.arch) + '</div></nav>';
  return titlebar() + '<div class="body">' + rail + '<main class="pane" id="pane">' + STEPS[S.step][1]() + '</main></div>' + footer();
}

function footer() {
  var busy = S.running;
  var left = '<button type="button" class="btn" data-act="cancel"' + attr('disabled', true, busy) + '>' + esc(t('gui.btn.cancel')) + '</button>';
  var alert = S.error ? '<span class="alert" role="alert">' + esc(S.error) + '</span>' : '<span class="sp"></span>';
  var back = S.step > 0 && !(S.finished && !S.installErr) ? '<button type="button" class="btn" data-act="back"' + attr('disabled', true, busy) + '>' + UI.icon('arrow-left') + esc(t('gui.btn.back')) + '</button>' : '';
  var next;
  if (S.step < STEP_INSTALL) {
    next = '<button type="button" class="btn pri" data-act="next">' + esc(t('gui.btn.next')) + UI.icon('arrow-right') + '</button>';
  } else if (S.finished && !S.installErr) {
    if (S.finishErr) {
      next = '<button type="button" class="btn" data-act="finish" data-k="finish">' + UI.icon('refresh') + esc(t('gui.btn.retry')) + '</button>' +
        '<button type="button" class="btn pri" data-act="quit" data-k="closeanyway">' + UI.icon('check') + esc(t('gui.btn.closeanyway')) + '</button>';
    } else {
      next = '<button type="button" class="btn pri" data-act="finish" data-k="finish"' + attr('disabled', true, S.finishing) + '>' + (S.finishing ? spinner() + esc(t('gui.btn.opening')) : UI.icon('check') + esc(t('gui.btn.finish'))) + '</button>';
    }
  } else if (S.finished) {
    next = '<button type="button" class="btn pri" data-act="replan">' + UI.icon('refresh') + esc(t('gui.btn.retry')) + '</button>';
  } else if (S.planErr) {
    next = '<button type="button" class="btn pri" data-act="replan" data-k="replan">' + UI.icon('refresh') + esc(t('gui.btn.retry')) + '</button>';
  } else if (S.running) {
    next = '<button type="button" class="btn pri" data-act="install" data-k="install" disabled aria-busy="true">' + spinner() + esc(t('gui.btn.installing')) + '</button>';
  } else if (!S.plan) {
    next = '<button type="button" class="btn pri" data-act="install" data-k="install" disabled aria-busy="true">' + spinner() + esc(t('gui.btn.preparing')) + '</button>';
  } else {
    next = '<button type="button" class="btn pri" data-act="install" data-k="install">' + UI.icon('download') + esc(t('gui.btn.install')) + '</button>';
  }
  if (S.finished && !S.installErr) left = '';
  return '<div class="ft">' + left + alert + back + next + '</div>';
}

// spinner is the small rotating ring shown inside a busy button.
function spinner() { return '<i class="spin" aria-hidden="true"></i>'; }

// stepHead is the header of a wizard step: its icon, "Step n of N", title and sentence.
function stepHead(title, lead) {
  return phead(STEPS[S.step][2], title, lead, { step: true });
}

// ---- screens ----------------------------------------------------------------

// intro is the animated "what is GabitoTL for": a small diagram (you, your agents,
// memory and code index, a pull request) and four beats that appear in sequence.
var introStart = 0;
function intro() {
  // The animation plays once. A re-render (language, license checkbox) continues
  // it where it was (negative delay) or, once it ended, shows the final state.
  if (!introStart) introStart = Date.now();
  var el = (Date.now() - introStart) / 1000;
  var done = el > 4;
  var nodes = [['user', 'gui.intro.d.you'], ['bot', 'gui.intro.d.agents'], ['db', 'gui.intro.d.memory'], ['pr', 'gui.intro.d.pr']];
  var flow = nodes.map(function (n, i) {
    return (i ? '<span class="arrow" style="--i:' + i + '"><i></i></span>' : '') +
      '<div class="node" style="--i:' + i + '"><span class="oic">' + UI.icon(n[0], 'lg') + '</span><span>' + esc(t(n[1])) + '</span></div>';
  }).join('');
  var beats = [['bot', 1, 'gui.intro.b1.title', 'gui.intro.b1.desc'], ['db', 2, 'gui.intro.b2.title', 'gui.intro.b2.desc'], ['search', 3, 'gui.intro.b3.title', 'gui.intro.b3.desc'], ['pr', 4, 'gui.intro.b4.title', 'gui.intro.b4.desc']].map(function (b) {
    return '<li class="beat" style="--i:' + b[1] + '"><span class="oic sm">' + UI.icon(b[0]) + '</span><div><b>' + esc(t(b[2])) + '</b>' + esc(t(b[3])) + '</div></li>';
  }).join('');
  return '<section class="intro' + (done ? ' done' : '') + '" style="--el:' + el.toFixed(2) + 's" aria-label="' + esc(t('gui.intro.title')) + '"><div class="lbl">' + UI.icon('spark') + esc(t('gui.intro.title')) + '</div>' +
    '<div class="flow" aria-hidden="true">' + flow + '</div><ol class="beats">' + beats + '</ol>' +
    '<p class="lead sm manage">' + esc(t('gui.intro.manage')) + '</p></section>';
}

function stepWelcome() {
  return '<div class="phead" style="gap:18px;margin-top:4px">' + logo(64) + '<div><div class="stepof">' + esc(t('gui.wiz.stepof', { n: 1, total: STEPS.length })) + '</div><h1>' + esc(t('gui.welcome.title')) + '</h1><p class="lead">' + esc(t('gui.welcome.tagline')) + '</p></div></div>' +
    '<p class="lead">' + esc(t('gui.welcome.lead')) + '</p>' +
    intro() +
    '<div class="lbl">' + UI.icon('globe') + esc(t('gui.welcome.language')) + '</div>' +
    '<div class="grid2">' + [['es', 'gui.welcome.lang.es.name', 'gui.welcome.lang.es.desc'], ['en', 'gui.welcome.lang.en.name', 'gui.welcome.lang.en.desc']].map(function (o) {
      return '<div class="opt' + (S.lang === o[0] ? ' sel' : '') + '"><label><input type="radio" name="lang" data-act="lang" data-val="' + o[0] + '" data-k="welcome-lang-' + o[0] + '"' + attr('checked', true, S.lang === o[0]) + '><span class="oic">' + UI.icon('globe', 'lg') + '</span><div><b>' + esc(t(o[1])) + '</b><span>' + esc(t(o[2])) + '</span></div></label></div>';
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
  var head = '<div class="hrow"><div class="grow">' + stepHead(t('gui.detect.title'), t('gui.detect.lead')) + '</div>' +
    '<button type="button" class="btn sm" data-act="scan"' + attr('disabled', true, S.scanning) + '>' + UI.icon('refresh') + esc(t('gui.btn.scan')) + '</button></div>';
  var d = S.detect;
  if (S.detectErr) return head + '<p class="lead" role="alert">' + esc(S.detectErr) + '</p>';
  if (!d) return head + UI.skeleton() + '<p class="lead">' + esc(t('gui.detect.scanning')) + '</p>';
  var none = t('gui.detect.none');
  var rows = [];
  rows.push(row(t('gui.detect.git'), d.git.found ? (d.git.path || 'git') + (d.userName ? ' · ' + d.userName : '') : none, d.git.found ? badge('ok', t('gui.detect.badge.ready')) : badge('err', t('gui.detect.badge.missing')), 'branch'));
  d.runtimes.forEach(function (r) { rows.push(row(r.label, r.name, runtimeBadge(r.state), rtIcon(r.name))); });
  rows.push(row(t('gui.detect.engram'), d.engram.present ? String(d.engram.observations) : none, d.engram.present ? badge('ok', t('gui.detect.badge.found')) : badge('todo', t('gui.detect.badge.optional')), 'db'));
  rows.push(row(t('gui.detect.container'), d.containerEngine || none, d.containerEngine ? badge('ok', t('gui.detect.badge.found')) : badge('todo', t('gui.detect.badge.optional')), 'server'));
  rows.push(row(t('gui.detect.pkg'), d.packageManager || none, d.packageManager ? badge('ok', t('gui.detect.badge.ready')) : badge('todo', t('gui.detect.badge.optional')), 'download'));
  var out = head + '<div class="box">' + rows.join('') + '</div>';
  if (d.gentleAI.found) {
    out += '<div class="call" role="group" aria-label="Gentle AI"><b>' + esc(t('gui.detect.gentle')) + '</b><div class="opts">' +
      [['migrate', 'gui.detect.gentle.migrate'], ['keep', 'gui.detect.gentle.keep']].map(function (o) {
        return '<label class="chk"><input type="radio" name="g" value="' + o[0] + '" data-bind="a.gentleAction" data-k="gentle-' + o[0] + '"' + attr('checked', true, S.a.gentleAction === o[0]) + '>' + esc(t(o[1])) + '</label>';
      }).join('') + '</div></div>';
  }
  return out;
}

function row(label, value, badgeHTML, ic) {
  return '<div class="row">' + (ic ? '<span class="oic sm">' + UI.icon(ic) + '</span>' : '') + '<b>' + esc(label) + '</b><span class="mono">' + esc(value) + '</span><span class="sp"></span>' + badgeHTML + '</div>';
}

var MODE_CARDS = [['lite', 'gui.profile.mode.lite', 'bolt', 'mode-lite'], ['normal', 'gui.profile.mode.normal', 'chat', 'mode-normal'], ['full', 'gui.profile.mode.full', 'book', 'mode-full']];

// personaCards draws the three chat modes as cards bound to path.
function personaCards(path) {
  return '<div class="grid3">' + MODE_CARDS.map(function (m) { return radio(path, m[0], t(m[1]), '', { icon: m[2], pop: m[3] }); }).join('') + '</div>';
}

function stepProfile() {
  var d = S.detect || { runtimes: [], gentleAI: {} };
  var rts = d.runtimes.length ? '<div class="grid3">' + d.runtimes.map(function (r) {
    var on = !!S.a.runtimes[r.name];
    return '<div class="rt' + (on ? ' sel' : '') + '"' + UI.popAttr(r.name) + ' data-pk="rt-' + esc(r.name) + '"><label><input type="checkbox" data-bind="a.runtimes.' + esc(r.name) + '" data-k="rt-' + esc(r.name) + '"' + attr('checked', true, on) + '><span class="oic sm">' + UI.icon(rtIcon(r.name)) + '</span>' + esc(r.label) + '</label>' +
      '<select aria-label="' + esc(r.label + ' — ' + t('gui.profile.integrate')) + '" title="' + esc(t('gui.profile.scope.project.hint')) + '"><option selected>' + esc(t('gui.profile.scope.global')) + '</option><option disabled>' + esc(t('gui.profile.scope.project')) + '</option></select>' + UI.infoBtn(r.name) + '</div>';
  }).join('') + '</div><p class="hint">' + esc(t('gui.profile.scope.project.hint')) + '</p>' : '<p class="lead">' + esc(t('gui.profile.noruntimes')) + '</p>';
  var noGentle = !d.gentleAI.found;
  return stepHead(t('gui.profile.title'), t('gui.profile.lead')) +
    field('a.name', t('gui.profile.name'), { hint: t('gui.profile.name.hint') }) +
    '<div class="lbl">' + UI.icon('chat') + esc(t('gui.profile.mode')) + '</div>' + personaCards('a.personaMode') + '<p class="hint">' + esc(t('gui.profile.mode.hint')) + '</p>' +
    '<div class="lbl">' + UI.icon('plug') + esc(t('gui.profile.integrate')) + '</div>' + rts +
    '<div style="display:flex;flex-direction:column;gap:10px;margin-top:4px">' +
    check('a.migratePersona', t('gui.profile.migrate'), { disabled: noGentle, title: noGentle ? t('note.gentle-ai-not-found') : '' }) +
    check('a.retireGentleKiro', t('gui.profile.retire-kiro'), { disabled: !d.gentleAI.gentleKiroPresent, title: d.gentleAI.gentleKiroPresent ? '' : t('note.gentle-kiro-none') }) + '</div>';
}

function stepMemory() {
  var m = S.a.memory;
  var team = m.mode === 'team';
  var out = stepHead(t('gui.memory.title'), t('gui.memory.lead')) +
    '<div class="grid2">' + radio('a.memory.mode', 'solo', t('gui.memory.solo'), t('gui.memory.solo.desc'), { icon: 'laptop', pop: 'mem-solo' }) + radio('a.memory.mode', 'team', t('gui.memory.team'), t('gui.memory.team.desc'), { icon: 'users', pop: 'mem-team' }) + '</div>';
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

// Generic glyphs for the platforms (no brand logos).
var CRED_ICON = { github: 'cloud', gitlab: 'diamond', bitbucket: 'bucket', 'bitbucket-server': 'server', server: 'server' };
var CRED_PLATFORMS = [['github', 'gui.delivery.platform.github'], ['gitlab', 'gui.delivery.platform.gitlab'], ['bitbucket', 'gui.delivery.platform.bitbucket']];
var CRED_STATUS = { saved: 'gui.delivery.cred.saved', rejected: 'gui.delivery.cred.rejected', unreachable: 'gui.delivery.cred.unreachable', invalid: 'gui.delivery.cred.invalid' };

function stepDelivery() {
  return stepHead(t('gui.delivery.title'), t('gui.delivery.lead')) + deliveryForm('a.delivery') + credentialsBlock();
}

// deliveryForm is the automation, merge method, draft and branch cleanup fields
// bound to the state object at path (the wizard, the Manager's global defaults
// and a project's override share it).
function deliveryForm(base) {
  var dl = get(base);
  var all = dl.automation === 'all';
  return '<div class="field">' + esc(t('gui.delivery.automation')) + '<div class="grid3">' +
    radio(base + '.automation', 'all', t('gui.delivery.auto.all'), t('gui.delivery.auto.all.desc'), { icon: 'merge', pop: 'auto-all' }) +
    radio(base + '.automation', 'until-pr', t('gui.delivery.auto.until-pr'), t('gui.delivery.auto.until-pr.desc'), { icon: 'pr', pop: 'auto-until-pr' }) +
    radio(base + '.automation', 'ask', t('gui.delivery.auto.ask'), t('gui.delivery.auto.ask.desc'), { icon: 'question', pop: 'auto-ask' }) + '</div></div>' +
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
  return '<button type="button" class="btn sm" data-act="' + act + '" data-k="' + act + '-' + esc(key) + '" ' + extra + (act === 'credtest' ? UI.popAttr('cred-test') : '') + attr('disabled', true, !!S.cred.busy) + '>' + esc(label) + '</button>';
}

// credentialsBlock is one row per platform: stored or not, a write-only token
// field and its outcome, then the self-hosted servers. The token is never read
// back from Go.
function credentialsBlock() {
  var c = S.cred;
  var head = '<div class="lbl"' + UI.popAttr('cred') + ' data-pk="cred-h">' + UI.icon('key') + esc(t('gui.delivery.creds.title')) + UI.infoBtn('cred') + '</div><p class="hint">' + esc(t('gui.delivery.creds.lead')) + '</p>';
  if (c.err) return head + '<p class="hint" role="alert">' + esc(c.err) + '</p>';
  return head + '<div class="box">' + CRED_PLATFORMS.map(function (p) {
    var info = (c.list || []).filter(function (x) { return x.platform === p[0]; })[0] || { host: '', stored: false };
    var busy = c.busy === p[0];
    var label = t(p[1]) + ' \u2014 ' + t('gui.delivery.cred.input');
    return '<div class="row"><span class="oic sm">' + UI.icon(CRED_ICON[p[0]]) + '</span><b>' + esc(t(p[1])) + '</b><span class="mono">' + esc(info.host) + '</span><span class="sp"></span>' +
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
  var out = '<div class="lbl"' + UI.popAttr('server') + ' data-pk="server-h">' + UI.icon('server') + esc(t('gui.servers.title')) + UI.infoBtn('server') + '</div><p class="hint">' + esc(t('gui.servers.lead')) + '</p><div class="box">';
  var list = c.servers || [];
  if (!list.length) out += '<div class="row"><span class="hint">' + esc(t('gui.servers.none')) + '</span></div>';
  list.forEach(function (s) {
    var key = 'srv:' + s.host;
    var d = 'data-cred="' + esc(s.platform) + '" data-host="' + esc(s.host) + '" ';
    out += '<div class="row"><span class="oic sm">' + UI.icon('server') + '</span><b>' + esc(s.host) + '</b><span>' + esc(srvPlatformLabel(s.platform)) + '</span><span class="mono">' + esc(s.url) + '</span><span class="sp"></span>' +
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
  var toggle = function (path, title, desc, ic, pop, o) {
    o = o || {};
    var on = !!get(path);
    return '<div class="tog' + (o.disabled ? ' off' : '') + (on ? ' sel' : '') + '"' + UI.popAttr(pop) + ' data-pk="' + path + '"><label class="tgl"><input type="checkbox" data-bind="' + path + '" data-k="' + path + '"' + attr('checked', true, on) + attr('disabled', true, !!o.disabled) + '><span class="oic sm">' + UI.icon(ic) + '</span><b>' + esc(title) + '</b><span>' + esc(desc) + '</span></label>' + UI.infoBtn(pop) + '</div>';
  };
  var cg = codegraphLines();
  return stepHead(t('gui.windows.title'), supported ? t('gui.windows.lead') : t('gui.windows.unsupported')) +
    '<div class="hrow">' + field('a.win.installDir', t('gui.windows.dir'), { mono: true, style: 'flex:1', disabled: !supported }) +
    '<button type="button" class="btn sm" data-act="browse"' + attr('disabled', true, !supported) + '>' + UI.icon('folder') + esc(t('gui.btn.browse')) + '</button></div>' +
    '<div class="lbl">' + UI.icon('desktop') + esc(t('gui.windows.shortcuts')) + '</div><div class="box">' +
    toggle('a.win.startMenu', t('gui.windows.startmenu'), t('gui.windows.startmenu.desc'), 'menu', 'startmenu', { disabled: !supported }) +
    toggle('a.win.desktopShortcut', t('gui.windows.desktop'), t('gui.windows.desktop.desc'), 'desktop', 'desktop', { disabled: !supported }) +
    toggle('a.win.addToPath', t('gui.windows.path'), t('gui.windows.path.desc'), 'terminal', 'path', { disabled: !supported }) + '</div>' +
    '<div class="lbl">' + UI.icon('lock') + esc(t('gui.windows.required')) + '</div><div class="box">' +
    '<div class="tog off sel"' + UI.popAttr('codegraph') + ' data-pk="cg"><label class="tgl"><input type="checkbox" checked disabled aria-describedby="cg-status"><span class="oic sm">' + UI.icon('graph') + '</span><b>' + esc(t('gui.windows.codegraph')) + '</b><span>' + esc(t('gui.windows.codegraph.desc')) + '</span></label>' + UI.infoBtn('codegraph') + '</div>' +
    '<div class="tog" id="cg-status" role="status"><span class="tgl" style="padding:10px 14px;color:var(--muted)">' + esc(cg.text) + (cg.shadow ? ' ' + esc(cg.shadow) : '') + '</span></div></div>';
}

function selectedRuntimes() {
  return ((S.detect && S.detect.runtimes) || []).filter(function (r) { return S.a.runtimes[r.name]; });
}

// doneHero is the success screen: a checkmark that draws itself and what to do next.
function doneHero() {
  var tips = [['desktop', 'gui.done.tip1.title', 'gui.done.tip1.desc'], ['spark', 'gui.done.tip2.title', 'gui.done.tip2.desc'], ['folders', 'gui.done.tip3.title', 'gui.done.tip3.desc']].map(function (f) {
    return '<div class="tip"><span class="oic sm">' + UI.icon(f[0]) + '</span><div><b>' + esc(t(f[1])) + '</b>' + esc(t(f[2])) + '</div></div>';
  }).join('');
  return '<div class="hero"><svg viewBox="0 0 64 64" aria-hidden="true" focusable="false"><circle class="ring" cx="32" cy="32" r="28"/><path class="tick" d="M19 33l9 9 17-19"/></svg>' +
    '<h1>' + esc(t('gui.install.done')) + '</h1><p class="lead">' + esc(t('gui.install.done.lead')) + '</p></div>' +
    '<div class="lbl">' + UI.icon('arrow-right') + esc(t('gui.done.next')) + '</div><div class="tips">' + tips + '</div>';
}

function stepInstall() {
  var ok = S.finished && !S.installErr;
  var title = S.finished ? (S.installErr ? t('gui.install.failed') : t('gui.install.done')) : (S.running ? t('gui.install.running') : t('gui.install.review'));
  var lead = ok ? t('gui.install.done.lead') : t('gui.install.lead');
  var out = ok ? '' : stepHead(S.running ? t('gui.install.title') : title, lead);
  if (S.planErr) return out + '<p class="lead" role="alert">' + esc(S.planErr) + '</p>';
  if (!S.plan) return out + '<div class="planning" role="status">' + spinner() + '<b>' + esc(t('gui.install.planning')) + '</b></div>' + UI.skeleton();
  if (ok) out += doneHero() + (S.finishErr ? '<div class="call" role="alert"><b>' + esc(t('gui.finish.failed', { detail: S.finishErr })) + '</b><span class="mono">' + esc(t('gui.finish.where', { path: S.a.win.installDir })) + '</span></div>' : '') + (S.result ? resultBlock() : '');
  var m = S.a.memory;
  var memText = m.mode === 'solo' ? t('gui.install.sum.memory.solo') : t(m.role === 'host' ? 'gui.install.sum.memory.host' : 'gui.install.sum.memory.client');
  var dl = S.a.delivery;
  var autoKey = { all: 'gui.delivery.auto.all', 'until-pr': 'gui.delivery.auto.until-pr', ask: 'gui.delivery.auto.ask' };
  var delivery = [t(autoKey[dl.automation]), t('gui.install.sum.delivery.global')];
  if (dl.draft) delivery.push(t('gui.delivery.draft'));
  var agents = selectedRuntimes().map(function (r) { return r.label; });
  out += '<dl class="sum panel">' +
    '<dt>' + esc(t('gui.install.sum.agents')) + '</dt><dd>' + esc(agents.length ? agents.join(', ') + ' (' + t('gui.install.sum.global') + ')' : t('gui.install.sum.none')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.memory')) + '</dt><dd>' + esc(memText) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.delivery')) + '</dt><dd>' + esc(delivery.join(' · ')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.creds')) + '</dt><dd>' + esc(t('gui.install.sum.creds.n', { n: storedCount() })) + '</dd>' +
    (S.cg && S.cg.supported ? '<dt>' + esc(t('gui.install.sum.codegraph')) + '</dt><dd>' + esc(t('gui.install.sum.codegraph.value', { version: S.cg.pinned })) + '</dd>' : '') +
    '<dt>' + esc(t('gui.install.sum.windows')) + '</dt><dd class="mono">' + esc(S.a.win.installDir || t('gui.install.sum.none')) + '</dd>' +
    '<dt>' + esc(t('gui.install.sum.backup')) + '</dt><dd>' + esc(S.plan.snapshot ? S.plan.snapshot : t('gui.install.sum.backup.value')) + '</dd></dl>';
  if (!S.running && !S.finished) return out + planDetails(S.plan);
  out += '<div style="display:flex;flex-direction:column;gap:8px"><div style="display:flex;font-size:13px"><b style="flex:1">' + esc(S.progressMsg) + '</b><span class="mono">' + S.pct + '%</span></div>' +
    '<div class="bar" role="progressbar" aria-label="' + esc(t('gui.install.progress')) + '" aria-valuenow="' + S.pct + '" aria-valuemin="0" aria-valuemax="100"><div data-gid="install" data-gv="' + S.pct + '" style="width:' + S.pct + '%"></div></div></div>' +
    '<div class="log mono" role="log" aria-label="' + esc(t('gui.install.log')) + '">' + S.log.map(logLine).join('') + '</div>';
  if (S.finished && !ok) out += resultBlock();
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

// dropPlan forgets the plan and the outcome of the last install attempt: going back
// changes answers, so the plan is always computed again on the way to Install.
function dropPlan() {
  S.plan = null;
  S.planErr = '';
  S.finished = false;
  S.installErr = '';
  S.result = null;
  S.log = [];
  S.pct = 0;
}

actions.back = function () {
  if (S.running) return;
  S.error = '';
  S.dir = 'back';
  S.step = Math.max(0, S.step - 1);
  dropPlan();
  render();
};

// goto jumps back to a finished step (its button in the stepper). Forward stays
// behind Next, which validates each screen.
actions.goto = function (el) {
  var i = parseInt(el.getAttribute('data-val'), 10);
  if (stepsLocked() || isNaN(i) || i < 0 || i >= S.step) return;
  S.error = '';
  S.dir = 'back';
  S.step = i;
  dropPlan();
  render();
};

actions.next = async function () {
  S.error = validate(S.step);
  if (S.error) return render();
  S.dir = 'fwd';
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
    S.planErr = t('gui.install.plan.failed', { detail: errText(e) });
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

// finish asks the backend to open the installed app and close this window. It is
// called once per click; on failure the page says why and offers "Close anyway".
actions.finish = async function () {
  if (S.finishing) return;
  S.finishing = true;
  S.finishErr = '';
  render();
  try {
    await api().Finish(S.a.win.installDir);
  } catch (e) {
    S.finishErr = errText(e);
    S.finishing = false;
    render();
  }
};
