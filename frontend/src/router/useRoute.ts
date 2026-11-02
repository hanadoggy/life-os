import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();

function notifyListeners() {
  for (const listener of listeners) {
    listener();
  }
}

if (typeof window !== "undefined") {
  window.addEventListener("popstate", notifyListeners);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function getSnapshot(): string {
  return window.location.pathname;
}

export function navigate(
  to: string,
  options: { replace?: boolean } = {}
): void {
  if (window.location.pathname === to) {
    return;
  }
  if (options.replace) {
    window.history.replaceState(null, "", to);
  } else {
    window.history.pushState(null, "", to);
  }
  notifyListeners();
}

export function useRoute(): string {
  return useSyncExternalStore(subscribe, getSnapshot, () => "/");
}

