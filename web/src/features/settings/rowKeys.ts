// List keys for rows that aren't saved yet (no id). Their place in the list won't do: taking
// a row out hands its key, and what the row's component held (a test's result), to the next one.
const keys = new WeakMap<object, string>();
let next = 1;

/** The row's id, or a key of its own until it has one. */
export function rowKey(row: { id?: string | number }): string {
  if (row.id != null) return String(row.id);
  let k = keys.get(row);
  if (!k) {
    k = `new-${next++}`;
    keys.set(row, k);
  }
  return k;
}

/** An edited copy of a row is still that row: it keeps the key. */
export function sameRow<T extends object>(from: object, to: T): T {
  const k = keys.get(from);
  if (k) keys.set(to, k);
  return to;
}
