import { use, useSyncExternalStore } from "react";

const resourceCache = new Map<string, Promise<unknown>>();
let cacheVersion = 0;
const subscribers = new Set<() => void>();

function subscribe(cb: () => void): () => void {
  subscribers.add(cb);
  return () => {
    subscribers.delete(cb);
  };
}

function getVersionSnapshot(): number {
  return cacheVersion;
}

/**
 * Invalidates cached promises matching the given prefix (or all resources if omitted)
 * and triggers a re-render of subscribed components.
 */
export function invalidateResources(prefix?: string): void {
  if (!prefix) {
    resourceCache.clear();
  } else {
    for (const key of resourceCache.keys()) {
      if (key.startsWith(prefix)) {
        resourceCache.delete(key);
      }
    }
  }
  cacheVersion++;
  for (const sub of subscribers) {
    sub();
  }
}

/**
 * React 19 `use()` wrapper with key-based promise caching and invalidation support.
 */
export function useResource<T>(key: string, fetcher: () => Promise<T>): T {
  const version = useSyncExternalStore(subscribe, getVersionSnapshot, () => 0);
  const versionedKey = `${key}@${version}`;

  let promise = resourceCache.get(versionedKey) as Promise<T> | undefined;
  if (!promise) {
    promise = fetcher();
    resourceCache.set(versionedKey, promise);
  }

  return use(promise);
}

