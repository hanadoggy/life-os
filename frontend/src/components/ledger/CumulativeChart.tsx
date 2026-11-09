import { useState } from "react";
import { TrendingDown, TrendingUp } from "lucide-react";
import { formatJPY, type DailyCumulativePoint } from "../../api/types";
import styles from "./CumulativeChart.module.css";

interface CumulativeChartProps {
  points: DailyCumulativePoint[];
  variableBudget?: number | null;
}

type ChartMode = "BURN_UP" | "BURN_DOWN";

export function CumulativeChart({
  points,
  variableBudget = null,
}: CumulativeChartProps) {
  const [mode, setMode] = useState<ChartMode>("BURN_UP");
  const [activeIndex, setActiveIndex] = useState<number | null>(null);

  const hasBudget = variableBudget !== null && variableBudget > 0;
  const effectiveMode: ChartMode = hasBudget ? mode : "BURN_UP";

  if (points.length === 0) {
    return (
      <section className={styles.card} aria-label="日別変動費累積推移">
        <div className={styles.header}>
          <div className={styles.titleGroup}>
            <TrendingUp size={18} strokeWidth={1.75} />
            <h2 className={styles.title}>変動費 累積推移 (日次)</h2>
          </div>
        </div>
        <div className={styles.emptyState}>データがありません</div>
      </section>
    );
  }

  const width = 640;
  const height = 190;
  const padLeft = 12;
  const padRight = 12;
  const padTop = 18;
  const padBottom = 26;
  const plotW = width - padLeft - padRight;
  const plotH = height - padTop - padBottom;

  const budgetVal = hasBudget && variableBudget !== null ? variableBudget : 0;

  function getX(index: number): number {
    if (points.length <= 1) return padLeft + plotW / 2;
    return padLeft + (index / (points.length - 1)) * plotW;
  }

  // Value calculation per mode
  const seriesValues = points.map((pt, i) => {
    const remaining = budgetVal - pt.cumulative;
    const idealRemaining =
      points.length > 1
        ? Math.round(budgetVal * (1 - i / (points.length - 1)))
        : 0;
    return {
      pt,
      value: effectiveMode === "BURN_DOWN" ? remaining : pt.cumulative,
      remaining,
      idealRemaining,
    };
  });

  const minVal =
    effectiveMode === "BURN_DOWN"
      ? Math.min(0, ...seriesValues.map((s) => s.value))
      : 0;

  const maxVal =
    effectiveMode === "BURN_DOWN"
      ? Math.max(1000, budgetVal, ...seriesValues.map((s) => s.value))
      : Math.max(1000, budgetVal, ...points.map((p) => p.cumulative));

  const valRange = Math.max(1, maxVal - minVal);

  function getY(val: number): number {
    return padTop + plotH - ((val - minVal) / valRange) * plotH;
  }

  const baselineY = getY(0);

  const coords = seriesValues.map((s, i) => ({
    x: getX(i),
    y: getY(s.value),
    ...s,
  }));

  const polylinePoints = coords
    .map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`)
    .join(" ");

  const areaPath =
    coords.length > 0
      ? `M ${coords[0].x.toFixed(1)} ${baselineY.toFixed(1)} ` +
        coords.map((c) => `L ${c.x.toFixed(1)} ${c.y.toFixed(1)}`).join(" ") +
        ` L ${coords[coords.length - 1].x.toFixed(1)} ${baselineY.toFixed(1)} Z`
      : "";

  const latestPoint = coords[coords.length - 1];
  const isOverBudget =
    hasBudget &&
    latestPoint !== undefined &&
    latestPoint.pt.cumulative > budgetVal;

  const displayIdx = activeIndex !== null ? activeIndex : points.length - 1;
  const activeCoord = coords[displayIdx] ?? coords[coords.length - 1];

  function handlePointerMove(e: React.PointerEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const relX = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const idx = Math.round(relX * (points.length - 1));
    setActiveIndex(idx);
  }

  return (
    <section className={styles.card} aria-label="日別変動費累積推移">
      <div className={styles.header}>
        <div className={styles.titleGroup}>
          {effectiveMode === "BURN_DOWN" ? (
            <TrendingDown size={18} strokeWidth={1.75} />
          ) : (
            <TrendingUp size={18} strokeWidth={1.75} />
          )}
          <h2 className={styles.title}>
            {effectiveMode === "BURN_DOWN"
              ? "変動費 予算残高推移"
              : "変動費 累積推移"}
          </h2>
        </div>

        <div className={styles.headerControls}>
          {activeCoord && (
            <div className={styles.readout}>
              <span className={`${styles.readoutDate} tabular`}>
                {activeCoord.pt.date.slice(5).replace("-", "/")}
              </span>
              <span className={styles.readoutLabel}>当日</span>
              <span className={`${styles.readoutDaily} tabular`}>
                {formatJPY(activeCoord.pt.amount)}
              </span>
              <span className={styles.readoutLabel}>
                {effectiveMode === "BURN_DOWN" ? "残高" : "累計"}
              </span>
              <span
                className={`${styles.readoutCumulative} ${
                  effectiveMode === "BURN_DOWN" && activeCoord.remaining < 0
                    ? styles.readoutOver
                    : ""
                } tabular`}
              >
                {formatJPY(
                  effectiveMode === "BURN_DOWN"
                    ? activeCoord.remaining
                    : activeCoord.pt.cumulative
                )}
              </span>
            </div>
          )}

          {hasBudget && (
            <div
              className={styles.modeTabs}
              role="tablist"
              aria-label="チャート表示モード切替"
            >
              <button
                type="button"
                role="tab"
                aria-selected={effectiveMode === "BURN_UP"}
                className={`${styles.modeBtn} ${
                  effectiveMode === "BURN_UP" ? styles.modeActive : ""
                }`}
                onClick={() => setMode("BURN_UP")}
              >
                累積
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={effectiveMode === "BURN_DOWN"}
                className={`${styles.modeBtn} ${
                  effectiveMode === "BURN_DOWN" ? styles.modeActive : ""
                }`}
                onClick={() => setMode("BURN_DOWN")}
              >
                残高
              </button>
            </div>
          )}
        </div>
      </div>

      <div className={styles.chartWrap}>
        <svg
          viewBox={`0 0 ${width} ${height}`}
          className={styles.svg}
          onPointerMove={handlePointerMove}
          onPointerLeave={() => setActiveIndex(null)}
        >
          {/* Horizontal reference grid lines */}
          {[0.25, 0.5, 0.75, 1].map((ratio) => {
            const y = padTop + plotH * (1 - ratio);
            return (
              <line
                key={ratio}
                x1={padLeft}
                y1={y}
                x2={width - padRight}
                y2={y}
                className={styles.gridLine}
              />
            );
          })}

          {/* Zero baseline when Burn-down goes negative */}
          {effectiveMode === "BURN_DOWN" && minVal < 0 && (
            <line
              x1={padLeft}
              y1={baselineY}
              x2={width - padRight}
              y2={baselineY}
              className={styles.zeroBaseline}
            />
          )}

          {/* Budget ceiling line in Burn-up mode */}
          {effectiveMode === "BURN_UP" && hasBudget && (
            <line
              x1={padLeft}
              y1={getY(budgetVal)}
              x2={width - padRight}
              y2={getY(budgetVal)}
              className={styles.idealLine}
            />
          )}

          {/* Ideal Burn-down pace guideline (day 1 budget -> last day 0) */}
          {effectiveMode === "BURN_DOWN" && hasBudget && coords.length > 1 && (
            <line
              x1={coords[0].x}
              y1={getY(budgetVal)}
              x2={coords[coords.length - 1].x}
              y2={baselineY}
              className={styles.idealLine}
            />
          )}

          <path
            d={areaPath}
            className={
              isOverBudget ? styles.areaFillOver : styles.areaFill
            }
          />

          <polyline
            fill="none"
            points={polylinePoints}
            className={
              isOverBudget ? styles.strokeLineOver : styles.strokeLine
            }
          />

          {activeCoord && (
            <>
              <line
                x1={activeCoord.x}
                y1={padTop}
                x2={activeCoord.x}
                y2={padTop + plotH}
                className={styles.cursorLine}
              />
              <circle
                cx={activeCoord.x}
                cy={activeCoord.y}
                r={4.5}
                className={styles.activeDot}
              />
            </>
          )}

          {/* X-axis labels: 1st, 10th, 20th, last day */}
          {coords
            .filter(
              (_, idx) =>
                idx === 0 ||
                idx === 9 ||
                idx === 19 ||
                idx === coords.length - 1
            )
            .map((c) => (
              <text
                key={c.pt.date}
                x={c.x}
                y={height - 6}
                textAnchor="middle"
                className={`${styles.axisLabel} tabular`}
              >
                {Number(c.pt.date.slice(8))}日
              </text>
            ))}
        </svg>
      </div>
    </section>
  );
}
