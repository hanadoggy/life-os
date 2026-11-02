import { useState } from "react";
import { TrendingUp } from "lucide-react";
import { formatJPY, type DailyCumulativePoint } from "../../api/types";
import styles from "./CumulativeChart.module.css";

interface CumulativeChartProps {
  points: DailyCumulativePoint[];
}

export function CumulativeChart({ points }: CumulativeChartProps) {
  const [activeIndex, setActiveIndex] = useState<number | null>(null);

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

  const maxVal = Math.max(
    1000,
    ...points.map((p) => p.cumulative)
  );

  function getX(index: number): number {
    if (points.length <= 1) return padLeft + plotW / 2;
    return padLeft + (index / (points.length - 1)) * plotW;
  }

  function getY(val: number): number {
    return padTop + plotH - (val / maxVal) * plotH;
  }

  const coords = points.map((pt, i) => ({
    x: getX(i),
    y: getY(pt.cumulative),
    pt,
  }));

  const polylinePoints = coords.map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`).join(" ");
  const areaPath =
    coords.length > 0
      ? `M ${coords[0].x.toFixed(1)} ${(padTop + plotH).toFixed(1)} ` +
        coords.map((c) => `L ${c.x.toFixed(1)} ${c.y.toFixed(1)}`).join(" ") +
        ` L ${coords[coords.length - 1].x.toFixed(1)} ${(padTop + plotH).toFixed(1)} Z`
      : "";

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
          <TrendingUp size={18} strokeWidth={1.75} />
          <h2 className={styles.title}>変動費 累積推移</h2>
        </div>

        {activeCoord && (
          <div className={styles.readout}>
            <span className={`${styles.readoutDate} tabular`}>
              {activeCoord.pt.date.slice(5).replace("-", "/")}
            </span>
            <span className={styles.readoutLabel}>当日</span>
            <span className={`${styles.readoutDaily} tabular`}>
              {formatJPY(activeCoord.pt.amount)}
            </span>
            <span className={styles.readoutLabel}>累計</span>
            <span className={`${styles.readoutCumulative} tabular`}>
              {formatJPY(activeCoord.pt.cumulative)}
            </span>
          </div>
        )}
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

          <path d={areaPath} className={styles.areaFill} />

          <polyline
            fill="none"
            points={polylinePoints}
            className={styles.strokeLine}
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

