import type { ReactNode } from "react";
import { useRoute } from "./useRoute";

export interface RouteDefinition {
  path: string;
  render: () => ReactNode;
}

export function Router({
  routes,
  fallback,
}: {
  routes: RouteDefinition[];
  fallback: () => ReactNode;
}) {
  const currentPath = useRoute();
  const matched = routes.find((r) => r.path === currentPath);
  return <>{matched ? matched.render() : fallback()}</>;
}

