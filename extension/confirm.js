// Confirmation window: the user decides whether a Game Vault address may read their session on
// some store hosts; the browser's own permission prompt follows Allow (it needs this click).
import { rememberAllowed } from './lib/capture.js';

const q = new URLSearchParams(location.search);
const id = q.get('id');
const origin = q.get('origin');
const hosts = (q.get('hosts') ?? '').split(',').filter(Boolean);
document.getElementById('what').textContent = `Game Vault at ${origin} wants your session on:`;
for (const h of hosts) {
  const li = document.createElement('li');
  li.textContent = h;
  document.getElementById('hosts').append(li);
}
// A plain-http address on the network asks every time (the background ignores Remember there).
if (!rememberAllowed(origin)) document.getElementById('remember-row').hidden = true;
const answer = async (allow) => {
  let granted = false;
  if (allow) granted = await chrome.permissions.request({ origins: hosts.map((h) => `https://${h}/*`) });
  await chrome.runtime.sendMessage({ op: 'confirm', id, allow: allow && granted, remember: document.getElementById('remember').checked });
  window.close();
};
document.getElementById('allow').onclick = () => answer(true);
document.getElementById('deny').onclick = () => answer(false);
