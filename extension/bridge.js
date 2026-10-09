// Content script on the Game Vault addresses the user enabled: relays the page's requests to the
// extension over a port (a port keeps the service worker alive through a long sign-in) and the
// answers back to the page. Only messages from this very page and origin are relayed.
(() => {
  if (window.__gamevaultConnectorBridge) return;
  window.__gamevaultConnectorBridge = true;
  const TYPE = 'gamevault-connector';
  const reply = (id, body) => window.postMessage({ ...body, type: TYPE, dir: 'response', id }, location.origin);

  window.addEventListener('message', (event) => {
    if (event.source !== window || event.origin !== location.origin) return;
    const d = event.data;
    if (!d || d.type !== TYPE || d.dir !== 'request' || typeof d.id !== 'string') return;
    const port = chrome.runtime.connect({ name: 'gamevault' });
    let answered = false;
    port.onMessage.addListener((msg) => {
      answered = true;
      reply(d.id, msg);
      port.disconnect();
    });
    port.onDisconnect.addListener(() => {
      if (!answered) reply(d.id, { op: 'error', code: 'failed', message: 'the extension stopped; try again' });
    });
    port.postMessage({ op: d.op, id: d.id, cancelId: d.cancelId, source: d.source, field: d.field, recipe: d.recipe });
  });
})();
