'use strict';
// i18n holds the text catalogue of the language on screen. The Go side owns the
// catalogues (messages_es-AR.txt and messages_en.txt); the page only looks keys
// up, so every visible string can be translated in one place.
(function () {
  var messages = {};

  function t(key, vars) {
    var s = messages[key];
    if (s === undefined) return key; // a gap stays visible instead of silent
    if (vars) {
      Object.keys(vars).forEach(function (k) {
        s = s.split('{' + k + '}').join(vars[k]);
      });
    }
    return s;
  }

  window.I18n = {
    set: function (m) { messages = m || {}; },
    t: t,
  };
  window.t = t;
})();
