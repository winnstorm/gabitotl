'use strict';
// ui.js is the shared look of the setup and the Manager: the icon helper (the
// sprite lives in index.html), the explanatory popover with its mini diagram,
// page transitions and the small animations that must play once, not on every
// re-render. It has no framework and no external asset (the CSP allows none).
// Everything dynamic goes through esc(); the markup built here is ours.

var UI = (function () {
  var reduced = false;
  try { reduced = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches); } catch (e) { /* no media queries: animate */ }

  // icon draws one sprite icon; size is a CSS class suffix (sm, lg, xl).
  function icon(name, size) {
    return '<svg class="i' + (size ? ' ' + size : '') + '" aria-hidden="true" focusable="false"><use href="#i-' + esc(name) + '"/></svg>';
  }

  // ---- popover content -----------------------------------------------------------
  // [id, icon of the title, the three icons of the mini diagram]. A trailing + marks
  // a good outcome (green), a trailing ! a blocked one (red). Every id has the
  // catalogue keys gui.pop.<id>.{title,what,when,changes,s1,s2,s3}; "changes" is a
  // list separated by " | " (the files and settings it touches).
  var POP_DEF = [
    ['claude-code', 'spark', 'user file check+'],
    ['opencode', 'terminal', 'user file check+'],
    ['kiro', 'ghost', 'user file check+'],
    ['mode-lite', 'bolt', 'chat bolt check+'],
    ['mode-normal', 'chat', 'chat chat check+'],
    ['mode-full', 'book', 'chat book check+'],
    ['mem-solo', 'laptop', 'spark db check+'],
    ['mem-team', 'users', 'users server check+'],
    ['auto-all', 'merge', 'branch pr merge+'],
    ['auto-until-pr', 'pr', 'branch pr user'],
    ['auto-ask', 'question', 'commit question check'],
    ['cred', 'key', 'key shield lock'],
    ['server', 'server', 'key server shield'],
    ['startmenu', 'menu', 'menu bot check+'],
    ['desktop', 'desktop', 'desktop bot check+'],
    ['path', 'terminal', 'terminal folder check+'],
    ['codegraph', 'graph', 'code graph spark'],
    ['gabito-design', 'palette', 'file palette check+'],
    ['int-connect', 'plug', 'plug file check+'],
    ['int-repair', 'wrench', 'warn wrench check+'],
    ['int-remove', 'trash', 'archive trash folder'],
    ['reindex', 'refresh', 'code refresh graph'],
    ['hooks', 'shield', 'commit shield ban!'],
    ['scan-history', 'search', 'commit search file'],
    ['restore', 'undo', 'archive undo check+'],
    ['cleanup', 'archive', 'archive trash check'],
    ['uninstall', 'trash', 'archive trash folder'],
    ['cred-test', 'key', 'key cloud check+'],
    ['project-setup', 'folder', 'folder file check+'],
    ['cg-fix', 'wrench', 'graph wrench check+'],
    ['eng-fix', 'wrench', 'db wrench check+'],
    ['forget', 'folders', 'folders x folder'],
    ['sys-gentle-ai', 'merge', 'search folder check+'],
    ['sys-superclaude', 'bot', 'search folder check+'],
    ['sys-spec-kit', 'file', 'search folder check+'],
    ['sys-openspec', 'file', 'search folder check+'],
    ['sys-bmad', 'users', 'search folder check+'],
    ['sys-task-master', 'commit', 'search folder check+'],
    ['sys-ruflo', 'graph', 'search folder check+'],
    ['sys-omo', 'terminal', 'search folder check+'],
    ['sys-ccpm', 'pr', 'search folder check+'],
  ];
  var POPS = {};
  POP_DEF.forEach(function (d) { POPS[d[0]] = { icon: d[1], flow: d[2].split(' ') }; });

  // popKey names a text of popover id; catalogue_test.go checks them all.
  function popKey(id, part) { return 'gui.pop.' + id + '.' + part; }

  function flowHTML(id) {
    var out = '';
    POPS[id].flow.forEach(function (ic, i) {
      var tone = /\+$/.test(ic) ? ' good' : (/!$/.test(ic) ? ' bad' : '');
      var name = ic.replace(/[+!]$/, '');
      if (i) out += '<span class="fa" aria-hidden="true">' + icon('arrow-right') + '</span>';
      out += '<div class="fn' + tone + '" style="animation-delay:' + (i * 120) + 'ms"><span class="fi">' + icon(name, 'lg') + '</span><span>' + esc(t(popKey(id, 's' + (i + 1)))) + '</span></div>';
    });
    return '<div class="flow">' + out + '</div>';
  }

  function popHTML(id) {
    var p = POPS[id];
    var changes = String(t(popKey(id, 'changes'))).split(' | ').map(function (c) { return '<li>' + esc(c) + '</li>'; }).join('');
    return '<div class="pop-h"><span class="pico">' + icon(p.icon, 'lg') + '</span><b id="gpop-t">' + esc(t(popKey(id, 'title'))) + '</b></div>' +
      flowHTML(id) +
      '<div class="pop-s"><h4>' + esc(t('gui.pop.h.what')) + '</h4><p>' + esc(t(popKey(id, 'what'))) + '</p></div>' +
      '<div class="pop-s"><h4>' + esc(t('gui.pop.h.when')) + '</h4><p>' + esc(t(popKey(id, 'when'))) + '</p></div>' +
      '<div class="pop-s"><h4>' + esc(t('gui.pop.h.changes')) + '</h4><ul class="mono">' + changes + '</ul></div>';
  }

  // infoBtn is the (i) button of a card: the way to the popover by touch or keyboard.
  function infoBtn(id) {
    if (!POPS[id]) return '';
    return '<button type="button" class="pinfo" data-popbtn="' + esc(id) + '" aria-haspopup="dialog" aria-expanded="false" aria-controls="gpop" aria-label="' + esc(t('gui.pop.info', { name: t(popKey(id, 'title')) })) + '">' + icon('info') + '</button>';
  }

  // popAttr marks an element as a popover trigger.
  function popAttr(id) { return POPS[id] ? ' data-pop="' + esc(id) + '"' : ''; }

  // ---- other agent systems -----------------------------------------------------------
  // systemsCard draws the "Other agent systems" list: every system found, possible
  // or not found, with an expandable "where we looked". scope is 'wizard' (global,
  // Gentle AI has its own decision), 'global' or 'project'. open is S.sysOpen.
  var SYS_ACTION = { coexist: 'gui.sys.action.coexist', review: 'gui.sys.action.review', decide: 'gui.sys.action.decide' };
  var SYS_KIND = { binary: 'gui.sys.kind.binary', dir: 'gui.sys.kind.dir', file: 'gui.sys.kind.file', marker: 'gui.sys.kind.marker', glob: 'gui.sys.kind.glob', dirs: 'gui.sys.kind.dirs' };
  function systemsCard(list, scope, open) {
    var rows = (Array.isArray(list) ? list : []).map(function (sy) {
      var st = sy.found ? badge('ok', t('gui.sys.found')) : (sy.possible ? badge('warn', t('gui.sys.possible')) : badge('todo', t('gui.sys.none')));
      var action = sy.action === 'decide' && scope !== 'wizard' ? 'coexist' : sy.action;
      var note = sy.found || sy.possible ? t(SYS_ACTION[action] || SYS_ACTION.coexist) : t(scope === 'project' ? 'gui.sys.absent.project' : 'gui.sys.absent');
      var key = scope + ':' + sy.id;
      var isOpen = !!(open && open[key]);
      var where = '';
      if (isOpen) {
        where = '<ul class="where mono">' + (sy.signals || []).map(function (g) {
          var ic = g.found ? '<span class="ic ok" role="img" aria-label="' + esc(t('gui.sys.hit')) + '">' + icon('check', 'sm') + '</span>' : '<span class="ic todo" role="img" aria-label="' + esc(t('gui.sys.miss')) + '">' + icon('minus', 'sm') + '</span>';
          var what = g.kind === 'binary' ? g.target : (g.path || g.target);
          return '<li>' + ic + '<span>' + esc(t(SYS_KIND[g.kind] || SYS_KIND.file)) + ': ' + esc(what) + (g.marker ? ' (' + esc(g.marker) + ')' : '') + (g.strong ? '' : ' - ' + esc(t('gui.sys.weak'))) + '</span></li>';
        }).join('') + '</ul>';
      }
      return '<div class="sys" data-sys="' + esc(sy.id) + '"><div class="row"' + popAttr('sys-' + sy.id) + '><b>' + esc(sy.name) + '</b><span class="sysnote">' + esc(note) + '</span>' + st +
        '<button type="button" class="btn xs" data-act="sysopen" data-val="' + esc(key) + '" data-k="sys-' + esc(key) + '" aria-expanded="' + isOpen + '">' + esc(t(isOpen ? 'gui.sys.hide' : 'gui.sys.where')) + '</button>' + infoBtn('sys-' + sy.id) + '</div>' + where + '</div>';
    }).join('');
    return '<section class="syscard" aria-label="' + esc(t('gui.sys.title')) + '"><div class="lbl">' + icon('search') + esc(t('gui.sys.title')) + '</div><p class="hint">' + esc(t(scope === 'project' ? 'gui.sys.lead.project' : 'gui.sys.lead')) + '</p>' +
      '<div class="box list">' + (rows || '<div class="row">' + esc(t('gui.sys.empty')) + '</div>') + '</div></section>';
  }

  // ---- popover behaviour -----------------------------------------------------------
  var pop = null, cur = null, openT = 0, closeT = 0;

  function ensure() {
    if (pop) return pop;
    pop = document.createElement('div');
    pop.id = 'gpop';
    pop.className = 'pop';
    pop.setAttribute('role', 'dialog');
    pop.setAttribute('aria-labelledby', 'gpop-t');
    pop.setAttribute('hidden', '');
    pop.addEventListener('mouseenter', function () { clearTimeout(closeT); });
    pop.addEventListener('mouseleave', function () { if (cur && cur.by === 'hover') scheduleClose(); });
    document.body.appendChild(pop);
    return pop;
  }

  function trigOf(node) { return node && node.closest ? node.closest('[data-pop]') : null; }

  function mark(tr, on) {
    if (!tr || !tr.setAttribute) return;
    if (on) tr.setAttribute('aria-describedby', 'gpop'); else tr.removeAttribute('aria-describedby');
    var b = tr.querySelector && tr.querySelector('[data-popbtn]');
    if (b) b.setAttribute('aria-expanded', on ? 'true' : 'false');
  }

  function place(tr) {
    var r = tr.getBoundingClientRect();
    var vw = window.innerWidth, vh = window.innerHeight, gap = 8, m = 8;
    pop.style.maxHeight = (vh - 2 * m) + 'px';
    pop.style.left = '0px';
    pop.style.top = '0px';
    var pw = pop.offsetWidth, ph = pop.offsetHeight;
    var below = vh - r.bottom - gap - m, above = r.top - gap - m;
    var left, top;
    if (Math.max(below, above) < Math.min(ph, 220)) {
      // No room above or below: sit beside the card, inside the window.
      left = r.right + gap + pw + m <= vw ? r.right + gap : Math.max(m, r.left - gap - pw);
      top = Math.min(Math.max(m, r.top), Math.max(m, vh - ph - m));
      pop.setAttribute('data-side', 'side');
    } else {
      var down = below >= ph || below >= above;
      var room = down ? below : above;
      var h = Math.min(ph, room);
      pop.style.maxHeight = h + 'px';
      top = down ? r.bottom + gap : r.top - gap - h;
      left = Math.min(Math.max(m, r.left), Math.max(m, vw - pw - m));
      pop.setAttribute('data-side', down ? 'below' : 'above');
    }
    pop.style.left = Math.round(left) + 'px';
    pop.style.top = Math.round(top) + 'px';
  }

  function open(tr, by) {
    var id = tr.getAttribute('data-pop');
    if (!POPS[id]) return;
    clearTimeout(openT);
    clearTimeout(closeT);
    ensure();
    if (cur && cur.trigger !== tr) mark(cur.trigger, false);
    var same = cur && cur.trigger === tr && !pop.hasAttribute('hidden');
    cur = { id: id, ka: tr.hasAttribute('data-pk') ? 'data-pk' : 'data-k', k: tr.getAttribute('data-pk') || tr.getAttribute('data-k') || '', trigger: tr, by: by, sig: t('gui.pop.h.what') };
    if (same) return;
    pop.innerHTML = popHTML(id);
    pop.removeAttribute('hidden');
    pop.classList.remove('show');
    place(tr);
    void pop.offsetWidth;
    pop.classList.add('show');
    mark(tr, true);
  }

  function close() {
    clearTimeout(openT);
    clearTimeout(closeT);
    if (cur) mark(cur.trigger, false);
    cur = null;
    if (pop) { pop.setAttribute('hidden', ''); pop.classList.remove('show'); }
  }

  function scheduleClose() {
    clearTimeout(closeT);
    closeT = setTimeout(close, 220);
  }

  document.addEventListener('mouseover', function (ev) {
    var tr = trigOf(ev.target);
    if (tr) {
      clearTimeout(closeT);
      if (cur && cur.trigger === tr) return;
      clearTimeout(openT);
      openT = setTimeout(function () { open(tr, 'hover'); }, cur ? 60 : 220);
      return;
    }
    clearTimeout(openT);
    if (pop && pop.contains(ev.target)) { clearTimeout(closeT); return; }
    if (cur && cur.by === 'hover') scheduleClose();
  });

  document.addEventListener('focusin', function (ev) {
    var tr = trigOf(ev.target);
    if (tr) { open(tr, 'focus'); return; }
    if (cur && cur.by === 'focus' && !(pop && pop.contains(ev.target))) close();
  });

  document.addEventListener('focusout', function (ev) {
    if (!cur || cur.by === 'btn') return;
    var to = ev.relatedTarget;
    if (to && (cur.trigger.contains(to) || (pop && pop.contains(to)))) return;
    if (cur.by === 'focus') close();
  });

  document.addEventListener('click', function (ev) {
    var b = ev.target.closest && ev.target.closest('[data-popbtn]');
    if (!b) return;
    var tr = trigOf(b);
    if (!tr) return;
    ev.preventDefault();
    if (cur && cur.trigger === tr && cur.by === 'btn') { close(); return; }
    close();
    open(tr, 'btn');
  });

  document.addEventListener('mousedown', function (ev) {
    if (!cur) return;
    if ((pop && pop.contains(ev.target)) || cur.trigger.contains(ev.target)) return;
    close();
  }, true);

  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape' && cur) {
      var tr = cur.trigger;
      close();
      ev.preventDefault();
      ev.stopImmediatePropagation();
      if (tr && tr.contains && !tr.contains(document.activeElement) && tr.querySelector) {
        var f = tr.querySelector('input, button');
        if (f && f.focus) f.focus();
      }
    }
  });

  window.addEventListener('resize', function () { if (cur && pop) place(cur.trigger); });
  window.addEventListener('scroll', function (ev) { if (cur && pop && !pop.contains(ev.target)) place(cur.trigger); }, true);

  // ---- once-per-change animations ------------------------------------------------------
  var last = { page: null, modal: false, toast: '' };
  var grow = {};
  var pulseKey = '';

  // pulse marks the control whose card should pop after the next render.
  function pulse(k) { pulseKey = k || ''; }

  // afterRender runs after every render. page identifies the screen (a change
  // slides/fades it in, in the direction dir), modal and toast play their own
  // entrance only when they appear. Elements are rebuilt on every render, so a
  // class added here plays its CSS animation exactly once per change.
  function afterRender(page, dir, modal, toast) {
    var pane = document.getElementById('pane');
    var changed = page !== last.page;
    if (changed && !reduced) {
      if (pane) pane.className = pane.className.replace(/\b(go|enter-\w+)\b/g, '') + ' go enter-' + (dir || 'fade');
      var body = document.querySelector('.body');
      if (body) body.className += ' go';
    }
    last.page = page;
    if (modal && !last.modal) close();
    if (modal && !last.modal && !reduced) {
      var back = document.querySelector('.back');
      if (back) back.className += ' go';
    }
    last.modal = !!modal;
    if (toast && toast !== last.toast && !reduced) {
      var tt = document.querySelector('.toast');
      if (tt) tt.className += ' go';
    }
    last.toast = toast || '';
    // Bars and the stepper line grow from where they were instead of jumping.
    Array.prototype.forEach.call(document.querySelectorAll('[data-gid]'), function (n) {
      var id = n.getAttribute('data-gid'), to = parseFloat(n.getAttribute('data-gv')), prop = n.getAttribute('data-gp') || 'width';
      var from = grow[id];
      grow[id] = to;
      if (reduced || from === undefined || from === to || isNaN(to)) return;
      n.style[prop] = from + '%';
      void n.offsetWidth;
      n.style[prop] = to + '%';
    });
    if (pulseKey) {
      var k = pulseKey;
      pulseKey = '';
      var el = document.querySelector('[data-k="' + k.replace(/"/g, '\\"') + '"]');
      var card = el && el.closest && el.closest('.opt, .rt, .tog');
      if (card && !reduced) card.className += ' pulse';
    }
    // The popover follows its trigger across a re-render, or closes with it.
    if (cur) {
      var again = document.querySelector('[data-pop="' + cur.id + '"]' + (cur.k ? '[' + cur.ka + '="' + cur.k.replace(/"/g, '\\"') + '"]' : ''));
      if (again) {
        cur.trigger = again;
        mark(again, true);
        if (pop) {
          var sig = t('gui.pop.h.what');
          if (cur.sig !== sig) { pop.innerHTML = popHTML(cur.id); cur.sig = sig; }
          place(again);
        }
      } else {
        close();
      }
    }
  }

  // emptyState is a small illustration, a sentence and an optional call to action.
  function emptyState(iconName, text, ctaHTML) {
    return '<div class="empty" role="status"><span class="ill" aria-hidden="true"><span class="ill-bg"></span><span class="ill-ic">' + icon(iconName, 'xl') + '</span><i class="d d1"></i><i class="d d2"></i><i class="d d3"></i></span>' +
      '<p>' + esc(text) + '</p>' + (ctaHTML || '') + '</div>';
  }

  // skeleton is the shimmering placeholder shown while a screen loads.
  function skeleton() {
    return '<div class="skel" role="status"><span class="sr">' + esc(t('gui.loading')) + '</span><i class="w60"></i><i class="w90"></i><i class="w75"></i><i class="w90"></i><i class="w40"></i></div>';
  }

  return { icon: icon, systemsCard: systemsCard, infoBtn: infoBtn, popAttr: popAttr, afterRender: afterRender, pulse: pulse, emptyState: emptyState, skeleton: skeleton, has: function (id) { return !!POPS[id]; }, ids: function () { return Object.keys(POPS); }, close: close };
})();
