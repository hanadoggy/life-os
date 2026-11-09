import { use, useSyncExternalStore } from "react";

const resourceCache = new Map<string, Promise<unknown>>();
const keyVersions = new Map<string, number>();
const subscribers = new Set<() => void>();

function subscribe(cb: () => void): () => void {
  subscribers.add(cb);
  return () => {
    subscribers.delete(cb);
  };
}

function getKeyVersion(key: string): number {
  return keyVersions.get(key) ?? 0;
}

function bumpKeyVersion(key: string): void {
  keyVersions.set(key, getKeyVersion(key) + 1);
}

/**
 * Invalidates cached promises matching the given prefix (or all resources if omitted)
 * and triggers a re-render only for components subscribed to the invalidated keys.
 */
export function invalidateResources(prefix?: string): void {
  if (!prefix) {
    for (const key of resourceCache.keys()) {
      bumpKeyVersion(key);
    }
    resourceCache.clear();
  } else {
    for (const key of Array.from(resourceCache.keys())) {
      if (key.startsWith(prefix)) {
        resourceCache.delete(key);
        bumpKeyVersion(key);
      }
    }
  }
  for (const sub of subscribers) {
    sub();
  }
}

/**
 * React 19 `use()` wrapper with key-based promise caching and selective invalidation support.
 */
export function useResource<T>(key: string, fetcher: () => Promise<T>): T {
  useSyncExternalStore(
    subscribe,
    () => getKeyVersion(key),
    () => 0
  );

  let promise = resourceCache.get(key) as Promise<T> | undefined;
  if (!promise) {
    promise = fetcher().catch((err) => {
      if (resourceCache.get(key) === promise) {
        resourceCache.delete(key);
      }
      throw err;
    });
    resourceCache.set(key, promise);
  }

  return use(promise);
}


