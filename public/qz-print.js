/*
 * QZ Tray client (Stage 31.1) - silent, one-click printing to a named OS printer.
 *
 * Why this file exists rather than the vendored qz-tray.js: that library is
 * ~150KB and carries its own promise shim, its own SHA-256, and remote-host
 * ("qz.surf") relay logic this deployment does not use. CLAUDE.md's first
 * principle is no new JS dependency where plain code will do, and the part we
 * actually need is a WebSocket, a hash and a signature.
 *
 * THE ONE THING THAT MUST NOT DRIFT
 * --------------------------------
 * QZ Tray verifies every privileged call like this (qz/auth/Certificate.java):
 *
 *     verifier.update(StringUtils.getBytesUtf8(DigestUtils.sha256Hex(data)));
 *     verifier.verify(Base64.decodeBase64(signature));      // SHA512withRSA
 *
 * where `data` is (qz/ws/PrintSocketClient.java):
 *
 *     new JSONObject(message, new String[] {"call", "params", "timestamp"}).toString()
 *
 * So the signed bytes are the UTF-8 bytes of the LOWERCASE HEX SHA-256 of the
 * JSON {"call":..,"params":..,"timestamp":..} - in that key order. We build
 * that string byte-for-byte the way the official client does, hash it here,
 * and the server signs the hex digest (engines/qz_print.go). Reordering those
 * three keys, or signing the raw digest bytes instead of the hex string,
 * produces a signature the tray rejects with no useful error - it just falls
 * back to prompting the operator on every single label.
 */
(function (global) {
  'use strict';

  // Verified against qz-tray.js `port: { secure: [...], insecure: [...] }`.
  var SECURE_PORTS = [8181, 8282, 8383, 8484];
  var INSECURE_PORTS = [8182, 8283, 8384, 8485];

  // localhost.qz.io is a public DNS name that resolves to 127.0.0.1, which is
  // how QZ ships a *valid* TLS certificate for a loopback connection - a
  // self-signed cert for "localhost" would make the browser refuse the socket
  // from an https:// page.
  var SECURE_HOST = 'localhost.qz.io';
  var INSECURE_HOST = 'localhost';

  var CONNECT_TIMEOUT_MS = 2000;
  var CALL_TIMEOUT_MS = 30000;

  // Mirrors qz-tray.js `_qz.printing.defaultConfig`. Sent in full rather than
  // as a partial object because the tray reads these keys directly; omitting
  // one makes it fall back to a driver default that differs per machine,
  // which is exactly the inconsistency this feature exists to remove.
  var DEFAULT_OPTIONS = {
    bounds: null, colorType: 'color', copies: 1, density: 0, duplex: false,
    fallbackDensity: null, interpolation: 'bicubic', jobName: null, legacy: false,
    margins: 0, orientation: null, paperThickness: null, printerTray: null,
    rasterize: false, rotation: 0, scaleContent: true, size: null, units: 'in',
    forceRaw: false, encoding: null, spool: null
  };

  /* ---------------------------------------------------------------- SHA-256
   * crypto.subtle is only exposed in a secure context. A packing bench on
   * http://192.168.x.x - the normal way this app is reached on a shop LAN -
   * has no crypto.subtle at all, so a pure-JS fallback is not optional here.
   */
  var K = new Uint32Array([
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
  ]);

  function utf8Bytes(str) {
    if (typeof TextEncoder !== 'undefined') return new TextEncoder().encode(str);
    var out = [], i, c;
    for (i = 0; i < str.length; i++) {
      c = str.charCodeAt(i);
      if (c < 0x80) out.push(c);
      else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
      else if (c < 0xd800 || c >= 0xe000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      else {
        i++;
        c = 0x10000 + (((c & 0x3ff) << 10) | (str.charCodeAt(i) & 0x3ff));
        out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      }
    }
    return new Uint8Array(out);
  }

  function sha256HexSync(str) {
    var msg = utf8Bytes(str);
    var bitLen = msg.length * 8;
    // Pad to a multiple of 64 bytes: 0x80, zeros, then a 64-bit big-endian length.
    var withPad = new Uint8Array((((msg.length + 8) >> 6) + 1) << 6);
    withPad.set(msg);
    withPad[msg.length] = 0x80;
    var dv = new DataView(withPad.buffer);
    dv.setUint32(withPad.length - 4, bitLen >>> 0, false);
    dv.setUint32(withPad.length - 8, Math.floor(bitLen / 0x100000000), false);

    var H = new Uint32Array([
      0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    ]);
    var w = new Uint32Array(64);

    for (var off = 0; off < withPad.length; off += 64) {
      var i;
      for (i = 0; i < 16; i++) w[i] = dv.getUint32(off + i * 4, false);
      for (i = 16; i < 64; i++) {
        var g0 = w[i - 15], g1 = w[i - 2];
        var s0 = ((g0 >>> 7) | (g0 << 25)) ^ ((g0 >>> 18) | (g0 << 14)) ^ (g0 >>> 3);
        var s1 = ((g1 >>> 17) | (g1 << 15)) ^ ((g1 >>> 19) | (g1 << 13)) ^ (g1 >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
      }
      var a = H[0], b = H[1], c = H[2], d = H[3], e = H[4], f = H[5], g = H[6], h = H[7];
      for (i = 0; i < 64; i++) {
        var S1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7));
        var ch = (e & f) ^ (~e & g);
        var t1 = (h + S1 + ch + K[i] + w[i]) >>> 0;
        var S0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10));
        var maj = (a & b) ^ (a & c) ^ (b & c);
        var t2 = (S0 + maj) >>> 0;
        h = g; g = f; f = e; e = (d + t1) >>> 0;
        d = c; c = b; b = a; a = (t1 + t2) >>> 0;
      }
      H[0] = (H[0] + a) >>> 0; H[1] = (H[1] + b) >>> 0; H[2] = (H[2] + c) >>> 0; H[3] = (H[3] + d) >>> 0;
      H[4] = (H[4] + e) >>> 0; H[5] = (H[5] + f) >>> 0; H[6] = (H[6] + g) >>> 0; H[7] = (H[7] + h) >>> 0;
    }

    var hex = '';
    for (var j = 0; j < 8; j++) hex += ('00000000' + H[j].toString(16)).slice(-8);
    return hex;
  }

  function sha256Hex(str) {
    if (global.crypto && global.crypto.subtle && global.crypto.subtle.digest) {
      return global.crypto.subtle.digest('SHA-256', utf8Bytes(str)).then(function (buf) {
        var bytes = new Uint8Array(buf), hex = '';
        for (var i = 0; i < bytes.length; i++) hex += ('0' + bytes[i].toString(16)).slice(-2);
        return hex;
      }).catch(function () {
        return sha256HexSync(str);
      });
    }
    return Promise.resolve(sha256HexSync(str));
  }

  /* ------------------------------------------------------------- connection */

  var socket = null;
  var connecting = null;
  var pending = {};          // uid -> {resolve, reject, timer}
  var certificatePEM = null;
  var trayVersion = null;

  function newUID() {
    // Same 6-char base36 shape qz-tray.js uses; only needs to be unique
    // among this page's in-flight calls.
    return (Math.random().toString(36) + '000000').slice(2, 8);
  }

  function candidates() {
    var list = [];
    var pageIsSecure = global.location && global.location.protocol === 'https:';
    var secure = SECURE_PORTS.map(function (p) { return 'wss://' + SECURE_HOST + ':' + p; });
    var insecure = INSECURE_PORTS.map(function (p) { return 'ws://' + INSECURE_HOST + ':' + p; });

    // An https:// page cannot open a ws:// socket at all (mixed content), so
    // there is no point trying. An http:// page prefers the plain socket:
    // it skips a TLS handshake and a DNS lookup of localhost.qz.io, which is
    // the difference between a print starting instantly and after ~2s.
    if (pageIsSecure) return secure;
    return insecure.concat(secure);
  }

  function openSocket(url) {
    return new Promise(function (resolve, reject) {
      var ws;
      try { ws = new WebSocket(url); } catch (e) { reject(e); return; }
      var settled = false;
      var timer = setTimeout(function () {
        if (settled) return;
        settled = true;
        try { ws.close(); } catch (e) { /* already gone */ }
        reject(new Error('timeout'));
      }, CONNECT_TIMEOUT_MS);

      ws.onopen = function () {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        resolve(ws);
      };
      ws.onerror = function () {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        reject(new Error('unreachable'));
      };
    });
  }

  function attachHandlers(ws) {
    ws.onmessage = function (evt) {
      var msg;
      try { msg = JSON.parse(evt.data); } catch (e) { return; }
      if (!msg.uid) return;                       // stream/event callback, unused here
      var entry = pending[msg.uid];
      if (!entry) return;
      delete pending[msg.uid];
      clearTimeout(entry.timer);
      if (msg.error) entry.reject(new Error(msg.error));
      else entry.resolve(msg.result);
    };
    ws.onclose = function () {
      socket = null;
      Object.keys(pending).forEach(function (uid) {
        clearTimeout(pending[uid].timer);
        pending[uid].reject(new Error('QZ Tray connection closed'));
        delete pending[uid];
      });
    };
    ws.onerror = function () { /* surfaced through onclose / call timeouts */ };
  }

  // Calls that the tray accepts unsigned. Mirrors qz-tray.js
  // `_qz.security.needsSigned`, which excludes getVersion and a handful of
  // teardown calls that never raise a dialog.
  var UNSIGNED_CALLS = {
    'getVersion': true, 'printers.getStatus': true, 'printers.stopListening': true,
    'usb.isClaimed': true, 'usb.closeStream': true, 'usb.releaseDevice': true,
    'hid.stopListening': true, 'hid.isClaimed': true, 'hid.closeStream': true,
    'hid.releaseDevice': true, 'file.stopListening': true
  };

  function fetchSignature(toSign) {
    return sha256Hex(toSign).then(function (hashHex) {
      // Only the 64-char digest leaves the browser - never the payload. A
      // 5MB marketplace label PDF is hashed locally and never re-uploaded
      // just to be signed.
      return apiFetch('/api/v1/print/qz/sign', {
        method: 'POST',
        body: JSON.stringify({ request: hashHex })
      });
    }).then(function (res) {
      if (!res || !res.ok) throw new Error('Could not sign the print request.');
      return res.json();
    });
  }

  function send(call, params) {
    if (!socket || socket.readyState !== 1) {
      return Promise.reject(new Error('QZ Tray is not connected'));
    }

    var msg = {
      call: call,
      params: params === undefined ? null : params,
      timestamp: Date.now()
    };

    var prepared;
    if (UNSIGNED_CALLS[call]) {
      prepared = Promise.resolve(msg);
    } else {
      // Build the signed string from exactly these three keys, in exactly
      // this order - see the header comment.
      var toSign = JSON.stringify({ call: msg.call, params: msg.params, timestamp: msg.timestamp });
      prepared = fetchSignature(toSign).then(function (signed) {
        msg.signature = signed.signature;
        msg.signAlgorithm = signed.algorithm;
        return msg;
      });
    }

    return prepared.then(function (finalMsg) {
      return dispatch(finalMsg);
    });
  }

  function dispatch(msg) {
    return new Promise(function (resolve, reject) {
      msg.uid = newUID();
      // The tray records which monitor asked, so any dialog it does raise
      // appears on the operator's screen rather than on monitor 1.
      msg.position = {
        x: typeof screen !== 'undefined' ? ((screen.availWidth || screen.width) / 2) : 0,
        y: typeof screen !== 'undefined' ? ((screen.availHeight || screen.height) / 2) : 0
      };
      pending[msg.uid] = {
        resolve: resolve,
        reject: reject,
        timer: setTimeout(function () {
          delete pending[msg.uid];
          reject(new Error('QZ Tray did not respond in time'));
        }, CALL_TIMEOUT_MS)
      };
      try {
        socket.send(JSON.stringify(msg));
      } catch (e) {
        clearTimeout(pending[msg.uid].timer);
        delete pending[msg.uid];
        reject(e);
      }
    });
  }

  function sendCertificate() {
    return new Promise(function (resolve, reject) {
      var msg = { certificate: certificatePEM, timestamp: Date.now() };
      msg.uid = newUID();
      pending[msg.uid] = {
        resolve: resolve,
        reject: reject,
        timer: setTimeout(function () {
          delete pending[msg.uid];
          // The tray acknowledges the certificate; if it never does, the
          // handshake is still usable but every call will prompt.
          resolve(null);
        }, CALL_TIMEOUT_MS)
      };
      socket.send(JSON.stringify(msg));
    });
  }

  function loadCertificate() {
    if (certificatePEM) return Promise.resolve(certificatePEM);
    return apiFetch('/api/v1/print/qz/certificate').then(function (res) {
      if (!res || !res.ok) throw new Error('Could not load the print certificate.');
      return res.json();
    }).then(function (data) {
      certificatePEM = data.certificate;
      return certificatePEM;
    });
  }

  function connect() {
    if (socket && socket.readyState === 1) return Promise.resolve(true);
    if (connecting) return connecting;

    var urls = candidates();
    connecting = loadCertificate().then(function () {
      return urls.reduce(function (chain, url) {
        return chain.catch(function () { return openSocket(url); });
      }, Promise.reject(new Error('start')));
    }).then(function (ws) {
      socket = ws;
      attachHandlers(ws);
      // Handshake order matters and matches the official client: an
      // unsigned getVersion first, then the certificate.
      return send('getVersion', null);
    }).then(function (version) {
      trayVersion = version;
      return sendCertificate();
    }).then(function () {
      connecting = null;
      return true;
    }).catch(function (err) {
      connecting = null;
      if (socket) { try { socket.close(); } catch (e) { /* noop */ } socket = null; }
      throw new Error('QZ Tray is not running or not reachable. Start QZ Tray on this PC and try again.');
    });

    return connecting;
  }

  function disconnect() {
    if (socket) { try { socket.close(); } catch (e) { /* noop */ } }
    socket = null;
  }

  /* ------------------------------------------------------------------- API */

  function listOSPrinters() {
    return connect().then(function () { return send('printers.find', { query: undefined }); });
  }

  function getDefaultPrinter() {
    return connect().then(function () { return send('printers.getDefault', null); });
  }

  /**
   * Sends an already-built data array to a named OS printer.
   * @param printerName exact OS name, as printers.find reports it
   * @param items       QZ data items (see engines/qz_payload.go)
   * @param copies      passed through as the driver's copy count
   */
  function printItems(printerName, items, copies) {
    if (!printerName) return Promise.reject(new Error('No printer selected.'));
    var options = Object.assign({}, DEFAULT_OPTIONS, { copies: copies > 0 ? copies : 1 });
    return connect().then(function () {
      return send('print', {
        printer: { name: printerName },
        options: options,
        data: items
      });
    });
  }

  global.QZPrint = {
    connect: connect,
    disconnect: disconnect,
    isConnected: function () { return !!socket && socket.readyState === 1; },
    version: function () { return trayVersion; },
    listOSPrinters: listOSPrinters,
    getDefaultPrinter: getDefaultPrinter,
    printItems: printItems,
    // Exposed for the self-test in the Configure Printers screen, which
    // proves the signing chain end-to-end before an operator relies on it.
    _sha256Hex: sha256Hex
  };
})(window);
