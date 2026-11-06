/**
 * subscriber is the subscribe function of a store whose listeners are kept in set, made once at module level: a new function on each
 * render would make useSyncExternalStore unsubscribe and subscribe again every time.
 */
export const subscriber = (set: Set<() => void>) => (listener: () => void) => {
  set.add(listener)
  return () => {
    set.delete(listener)
  }
}
