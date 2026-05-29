(function() {
  'use strict';
  try {
    var BEACON_URL = 'https://watchtower.attlas.uk/api/beacon';
    // v2 — reads attlas_user cookie, falls back to /api/me
    var ME_URL = 'https://attlas.uk/api/me';
    var SESSION_KEY = '__wt_sid';

    var sid = sessionStorage.getItem(SESSION_KEY);
    if (!sid) {
      sid = crypto.randomUUID ? crypto.randomUUID() :
        'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
          var r = Math.random() * 16 | 0;
          return (c === 'x' ? r : (r & 0x3 | 0x8)).toString(16);
        });
      sessionStorage.setItem(SESSION_KEY, sid);
    }

    function getCookie(name) {
      var m = document.cookie.match(new RegExp('(?:^|; )' + name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '=([^;]*)'));
      return m ? decodeURIComponent(m[1]) : null;
    }

    function getEmail() {
      var email = getCookie('attlas_user');
      if (email && email.indexOf('@') !== -1) return email;
      return null;
    }

    function getApp() {
      var host = location.hostname;
      if (host === 'attlas.uk' || host === 'www.attlas.uk') {
        var seg = location.pathname.split('/')[1];
        return seg || 'dashboard';
      }
      return host.split('.')[0];
    }

    var app = getApp();
    var loadTime = Date.now();

    function send(eventType, meta, email) {
      if (!email) return;
      var payload = JSON.stringify({
        email: email,
        app: app,
        origin: location.hostname,
        path: location.pathname,
        session_id: sid,
        event_type: eventType,
        meta: meta || '',
        timestamp: Date.now()
      });
      var blob = new Blob([payload], { type: 'text/plain' });
      if (navigator.sendBeacon) {
        navigator.sendBeacon(BEACON_URL, blob);
      } else {
        fetch(BEACON_URL, { method: 'POST', body: blob, keepalive: true });
      }
    }

    function sendEvent(eventType, meta) {
      var email = getEmail();
      if (email) {
        send(eventType, meta, email);
      }
    }

    // Try to get email — if cookie missing, fetch /api/me to set it
    var email = getEmail();
    if (email) {
      send('pageview', '', email);
    } else {
      fetch(ME_URL, { credentials: 'include' })
        .then(function(r) { return r.ok ? r.json() : null; })
        .then(function(d) {
          if (d && d.email) send('pageview', '', d.email);
        })
        .catch(function() {});
    }

    document.addEventListener('visibilitychange', function() {
      sendEvent('visibility', document.visibilityState);
    });

    window.addEventListener('pagehide', function() {
      sendEvent('unload', String(Date.now() - loadTime));
    });

  } catch(e) {}
})();
