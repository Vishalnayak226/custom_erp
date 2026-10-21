// Stage 125.2: the Control Panel's Open lands on a client's own address with
// a single-use code in the URL fragment - a fragment never reaches a server
// log or a Referer header. Spending it signs the platform operator in as the
// client's hidden support user: no password or authenticator in this client.
// A used or expired code falls back to the ordinary login screen.
// completeLogin and showLoginScreen are the shell's own (app.js).
export async function redeemOperatorHandoff(code) {
  try {
    // Plain fetch, no Authorization header: a stale session on this address
    // must not stop the code from being spent.
    const res = await fetch('/api/v1/auth/operator-handoff', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    });
    const data = await res.json().catch(() => ({}));
    if (res.ok && data.token) {
      completeLogin(data);
      return;
    }
  } catch (_) {
    // Network failure: the login screen below says what to do.
  }
  showLoginScreen();
  const errorEl = document.getElementById('login-error');
  if (errorEl) {
    errorEl.textContent = 'This link has expired or was already used. Open the client again from the Control Panel.';
    errorEl.classList.remove('hidden');
  }
}
