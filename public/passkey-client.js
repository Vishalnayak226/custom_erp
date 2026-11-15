// Passkeys (49.2.3): the browser half of WebAuthn. Lazy module: the sign-in
// screen loads it only when the person has a passkey, My Profile when opened.
const toB64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const fromB64 = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0));

export const passkeysSupported = () => !!(window.PublicKeyCredential && navigator.credentials);

function creationOptions(pk) {
  return {
    ...pk,
    challenge: fromB64(pk.challenge),
    user: { ...pk.user, id: fromB64(pk.user.id) },
    excludeCredentials: (pk.excludeCredentials || []).map((c) => ({ ...c, id: fromB64(c.id) }))
  };
}

// Adds a passkey to the signed-in account. apiFetch is the shell's own.
export async function addPasskey(apiFetch, label) {
  const o = await apiFetch('/api/v1/me/passkeys/options', { method: 'POST', body: '{}' });
  if (!o || !o.ok) return o;
  const cred = await navigator.credentials.create({ publicKey: creationOptions((await o.json()).publicKey) });
  return apiFetch('/api/v1/me/passkeys', {
    method: 'POST',
    body: JSON.stringify({
      label,
      client_data_json: toB64(cred.response.clientDataJSON),
      attestation_object: toB64(cred.response.attestationObject)
    })
  });
}

// Answers the MFA challenge with a passkey; resolves to the session data.
export async function passkeySignIn(challengeToken) {
  const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${challengeToken}` };
  const o = await fetch('/api/v1/auth/passkey/options', { method: 'POST', headers, body: '{}' });
  const od = await o.json();
  if (!o.ok) throw new Error(od.error || 'Passkey sign-in failed.');
  const pk = od.publicKey;
  const cred = await navigator.credentials.get({
    publicKey: { ...pk, challenge: fromB64(pk.challenge), allowCredentials: pk.allowCredentials.map((c) => ({ ...c, id: fromB64(c.id) })) }
  });
  const r = await fetch('/api/v1/auth/passkey/verify', {
    method: 'POST',
    headers,
    body: JSON.stringify({
      credential_id: toB64(cred.rawId),
      client_data_json: toB64(cred.response.clientDataJSON),
      authenticator_data: toB64(cred.response.authenticatorData),
      signature: toB64(cred.response.signature)
    })
  });
  const data = await r.json();
  if (!r.ok) throw new Error(data.error || 'Passkey sign-in failed.');
  return data;
}

// Puts a "Use a passkey" button on the MFA challenge screen.
export function offerPasskey(screen, challengeToken, onSession) {
  if (!passkeysSupported() || screen.querySelector('#passkey-signin-btn')) return;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.id = 'passkey-signin-btn';
  btn.className = 'btn btn-secondary';
  btn.textContent = 'Use a passkey';
  const err = document.getElementById('mfa-challenge-error');
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    try {
      onSession(await passkeySignIn(challengeToken));
    } catch (e) {
      err.textContent = e.name === 'NotAllowedError' ? 'Passkey cancelled.' : e.message;
      err.classList.remove('hidden');
    } finally {
      btn.disabled = false;
    }
  });
  screen.querySelector('form').appendChild(btn);
}

// My Profile: list, add and remove passkeys inside the given element.
export async function renderPasskeyPanel(panel, { apiFetch, showApiError, showCustomPrompt }) {
  const res = await apiFetch('/api/v1/me/passkeys');
  if (!res || !res.ok) return;
  const list = (await res.json()).passkeys || [];
  panel.innerHTML = '<h3 class="card-title">Passkeys</h3>';
  const ul = document.createElement('div');
  list.forEach((p) => {
    const row = document.createElement('div');
    const name = document.createElement('span');
    name.textContent = `${p.label} · ${p.last_used_at ? 'used ' + String(p.last_used_at).slice(0, 10) : 'not used yet'} `;
    const del = document.createElement('button');
    del.type = 'button';
    del.className = 'btn btn-secondary';
    del.textContent = 'Remove';
    del.addEventListener('click', async () => {
      const r = await apiFetch(`/api/v1/me/passkeys/${p.id}`, { method: 'DELETE' });
      if (r && !r.ok) return showApiError(r, 'Failed to remove the passkey.');
      await renderPasskeyPanel(panel, { apiFetch, showApiError, showCustomPrompt });
    });
    row.append(name, del);
    ul.appendChild(row);
  });
  panel.appendChild(ul);
  if (!passkeysSupported()) return;
  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'btn btn-secondary';
  add.textContent = 'Add a passkey';
  add.addEventListener('click', async () => {
    const label = await showCustomPrompt('Name this passkey', 'This device', 'Add a passkey');
    if (label === null) return;
    try {
      const r = await addPasskey(apiFetch, label);
      if (r && !r.ok) return showApiError(r, 'Failed to add the passkey.');
    } catch (e) {
      return showApiError({ ok: false, status: 0, json: async () => ({ error: e.name === 'NotAllowedError' ? 'Passkey cancelled.' : e.message }) }, 'Failed to add the passkey.');
    }
    await renderPasskeyPanel(panel, { apiFetch, showApiError, showCustomPrompt });
  });
  panel.appendChild(add);
}
