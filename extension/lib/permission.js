// Asking for a host permission from the popup.

/**
 * Saves what the background needs to finish once a permission is granted, and asks for it, both in
 * the turn of the click that called it: Firefox only shows its permission prompt while it is still
 * handling that click (an await before permissions.request makes it refuse), and it closes the popup
 * for the prompt, so the write must already be under way. Resolves to the browser's answer once the
 * write is done too.
 */
export function saveAndRequest(chrome, items, permissions) {
  const saved = chrome.storage.local.set(items);
  const granted = chrome.permissions.request(permissions);
  return Promise.all([saved, granted]).then(([, answer]) => answer);
}
