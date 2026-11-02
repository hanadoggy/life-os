import { useRef, useState, type PointerEvent } from "react";

/**
 * Pointer Events vertical swipe-down gesture hook for closing native <dialog> bottom sheets.
 */
export function useSwipeToClose(onClose: () => void, thresholdPx = 90) {
  const startYRef = useRef<number | null>(null);
  const [dragOffsetY, setDragOffsetY] = useState(0);

  function onPointerDown(e: PointerEvent<HTMLElement>) {
    startYRef.current = e.clientY;
    e.currentTarget.setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: PointerEvent<HTMLElement>) {
    if (startYRef.current === null) return;
    const delta = e.clientY - startYRef.current;
    setDragOffsetY(delta > 0 ? delta : 0);
  }

  function onPointerUp(e: PointerEvent<HTMLElement>) {
    if (startYRef.current === null) return;
    const delta = e.clientY - startYRef.current;
    startYRef.current = null;
    if (e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId);
    }
    if (delta >= thresholdPx) {
      setDragOffsetY(0);
      onClose();
    } else {
      setDragOffsetY(0);
    }
  }

  return {
    dragOffsetY,
    handleProps: {
      onPointerDown,
      onPointerMove,
      onPointerUp,
      onPointerCancel: onPointerUp,
    },
  };
}

