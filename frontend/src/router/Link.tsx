import type { AnchorHTMLAttributes, MouseEvent } from "react";
import { navigate } from "./useRoute";

export interface LinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  to: string;
  replace?: boolean;
}

export function Link({ to, replace, onClick, children, ...rest }: LinkProps) {
  function handleClick(e: MouseEvent<HTMLAnchorElement>) {
    onClick?.(e);
    if (
      e.defaultPrevented ||
      e.button !== 0 ||
      e.metaKey ||
      e.altKey ||
      e.ctrlKey ||
      e.shiftKey
    ) {
      return;
    }
    e.preventDefault();
    navigate(to, { replace });
  }

  return (
    <a href={to} onClick={handleClick} {...rest}>
      {children}
    </a>
  );
}

