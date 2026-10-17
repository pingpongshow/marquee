/**
 * True when a global keyboard shortcut should leave this key event alone: it comes from a
 * text field, select, slider or editable element, or a dropdown menu or modal dialog is open
 * and owns the keys.
 */
export function ignoreShortcut(e: KeyboardEvent): boolean {
  if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return true;
  const t = e.target;
  if (t instanceof HTMLElement && t !== document.body) {
    if (t.isContentEditable) return true;
    const tag = t.tagName;
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return true;
    if (
      t.closest(
        '[role="slider"],[role="menu"],[role="listbox"],[contenteditable="true"],dialog',
      )
    )
      return true;
  }
  // An open menu, or a modal dialog (it owns Space and Escape), takes the keys.
  return !!document.querySelector('[role="menu"],dialog[open]');
}
