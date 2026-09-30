// Registers the service worker and offers window.gvPush to the Settings
// "Push notifications" card (see internal/web/pushsettings.go). No inline
// scripts are used anywhere: the card calls these from Datastar expressions.
(function () {
  'use strict';

  var canWorker = 'serviceWorker' in navigator;
  if (canWorker && window.isSecureContext) {
    window.addEventListener('load', function () {
      navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(function () {});
    });
  }

  function isIOS() {
    return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  }
  function isInstalled() {
    return navigator.standalone === true || (window.matchMedia && matchMedia('(display-mode: standalone)').matches);
  }
  function b64ToBytes(s) {
    var pad = '='.repeat((4 - (s.length % 4)) % 4);
    var raw = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'));
    var out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }
  function sameKey(sub, key) {
    var k = sub.options && sub.options.applicationServerKey;
    if (!k) return false;
    var a = new Uint8Array(k);
    if (a.length !== key.length) return false;
    for (var i = 0; i < a.length; i++) if (a[i] !== key[i]) return false;
    return true;
  }
  function registration() {
    return navigator.serviceWorker.getRegistration('/').then(function (r) {
      return r || navigator.serviceWorker.register('/sw.js', { scope: '/' }).then(function () { return navigator.serviceWorker.ready; });
    });
  }

  window.gvPush = {
    // state resolves to one of: insecure, ios-install, unsupported, denied, off, on.
    state: function () {
      if (!window.isSecureContext) return Promise.resolve('insecure');
      if (isIOS() && !isInstalled()) return Promise.resolve('ios-install');
      if (!canWorker || !('PushManager' in window) || !('Notification' in window)) return Promise.resolve('unsupported');
      if (Notification.permission === 'denied') return Promise.resolve('denied');
      return registration()
        .then(function (r) { return r.pushManager.getSubscription(); })
        .then(function (s) { return s && Notification.permission === 'granted' ? 'on' : 'off'; })
        .catch(function () { return 'unsupported'; });
    },
    // enable asks permission (call it straight from a click) and subscribes.
    // It resolves to the subscription as JSON text for the server.
    enable: function (vapidKey) {
      var key = b64ToBytes(vapidKey);
      return Notification.requestPermission().then(function (p) {
        if (p !== 'granted') throw new Error('Notifications were not allowed. Allow them for this site in the browser settings.');
        return registration();
      }).then(function (reg) {
        return reg.pushManager.getSubscription().then(function (old) {
          if (old && !sameKey(old, key)) return old.unsubscribe().then(function () { return null; });
          return old;
        }).then(function (sub) {
          return sub || reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
        });
      }).then(function (sub) { return JSON.stringify(sub.toJSON()); });
    },
    // disable unsubscribes this browser; it resolves to the old endpoint
    // (empty when there was none) so the server can forget it.
    disable: function () {
      return registration()
        .then(function (r) { return r.pushManager.getSubscription(); })
        .then(function (s) {
          if (!s) return '';
          var ep = s.endpoint;
          return s.unsubscribe().then(function () { return ep; });
        });
    },
    // message turns a failure into text for the card.
    message: function (e) {
      return e && e.message ? e.message : 'Something went wrong.';
    },
  };
})();
