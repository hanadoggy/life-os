import { useState } from "react";
import { PieChart } from "lucide-react";
import {
  formatCategoryName,
  formatJPY,
  type CategoryBreakdownItem,
  type ExpenseType,
} from "../../api/types";
import styles from "./CategoryBreakdownCard.module.css";

interface CategoryBreakdownCardProps {
  breakdown: CategoryBreakdownItem[];
  selectedCategoryId: number | null;
  onToggleCategory: (categoryId: number) => void;
}

type BreakdownScope = ExpenseType | "ALL";

const CHART_COLORS = [
  "var(--chart-cat-1)",
  "var(--chart-cat-2)",
  "var(--chart-cat-3)",
  "var(--chart-cat-4)",
  "var(--chart-cat-5)",
  "var(--chart-cat-6)",
  "var(--chart-cat-7)",
  "var(--chart-cat-8)",
];

const SCOPE_LABELS: Record<BreakdownScope, string> = {
  VARIABLE: "変動費",
  FIXED: "固定費",
  ALL: "すべて",
};

interface ActiveRow {
  categoryId: number;
  categoryName: string;
  amount: number;
  txCount: number;
  pointsSaved: number;
  shareRatio: number;
  color: string;
}

export function CategoryBreakdownCard({
  breakdown,
  selectedCategoryId,
  onToggleCategory,
}: CategoryBreakdownCardProps) {
  const [scope, setScope] = useState<BreakdownScope>("VARIABLE");

  function getScopedItems(targetScope: BreakdownScope) {
    return breakdown
      .map((item) => {
        const amount =
          targetScope === "VARIABLE"
            ? item.variable_amount
            : targetScope === "FIXED"
              ? item.fixed_amount
              : item.total_my_share;
        const txCount =
          targetScope === "VARIABLE"
            ? item.variable_tx_count
            : targetScope === "FIXED"
              ? item.fixed_tx_count
              : item.total_tx_count;

        return {
          categoryId: item.category_id,
          categoryName: item.category_name,
          amount,
          txCount,
          pointsSaved: item.points_saved,
        };
      })
      .filter((item) => item.amount > 0)
      .sort((a, b) => b.amount - a.amount || a.categoryId - b.categoryId);
  }

  const scopedItems = getScopedItems(scope);

  function handleScopeChange(nextScope: BreakdownScope) {
    setScope(nextScope);
    if (selectedCategoryId !== null) {
      const nextItems = getScopedItems(nextScope);
      if (!nextItems.some((item) => item.categoryId === selectedCategoryId)) {
        onToggleCategory(selectedCategoryId);
      }
    }
  }

  const totalAmount = scopedItems.reduce((sum, item) => sum + item.amount, 0);

  const rows: ActiveRow[] = scopedItems.map((item, idx) => ({
    ...item,
    shareRatio: totalAmount > 0 ? item.amount / totalAmount : 0,
    color: CHART_COLORS[idx % CHART_COLORS.length],
  }));

  const selectedRow =
    selectedCategoryId !== null
      ? rows.find((r) => r.categoryId === selectedCategoryId) ?? null
      : null;

  // SVG Donut geometry
  const size = 188;
  const center = size / 2;
  const radius = 68;
  const circumference = 2 * Math.PI * radius;

  let cumulativeOffset = 0;
  const segments = rows.map((row) => {
    const length = row.shareRatio * circumference;
    const offset = cumulativeOffset;
    cumulativeOffset += length;
    return {
      ...row,
      strokeDasharray: `${Math.max(0, length)} ${circumference}`,
      strokeDashoffset: -offset,
    };
  });

  return (
    <section className={styles.card} aria-label="カテゴリ別支出分析">
      <div className={styles.header}>
        <div className={styles.titleGroup}>
          <PieChart size={18} strokeWidth={1.75} />
          <h2 className={styles.title}>カテゴリ別支出</h2>
        </div>

        <div className={styles.scopeTabs} role="tablist" aria-label="支出区分切替">
          {(["VARIABLE", "FIXED", "ALL"] as BreakdownScope[]).map((key) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={scope === key}
              className={`${styles.tabBtn} ${
                scope === key ? styles.tabActive : ""
              }`}
              onClick={() => handleScopeChange(key)}
            >
              {SCOPE_LABELS[key]}
            </button>
          ))}
        </div>
      </div>

      {rows.length === 0 ? (
        <div className={styles.emptyState}>
          該当する{SCOPE_LABELS[scope]}データがありません
        </div>
      ) : (
        <div className={styles.body}>
          <div className={styles.chartColumn}>
            <div className={styles.donutContainer}>
              <svg
                viewBox={`0 0 ${size} ${size}`}
                className={styles.donutSvg}
                role="img"
                aria-label="カテゴリ別支出円グラフ"
              >
                {/* Subtle track ring */}
                <circle
                  cx={center}
                  cy={center}
                  r={radius}
                  fill="none"
                  stroke="var(--border-subtle)"
                  strokeWidth={20}
                />

                <g transform={`rotate(-90 ${center} ${center})`}>
                  {segments.map((seg) => {
                    const isSelected = selectedCategoryId === seg.categoryId;
                    const isDimmed =
                      selectedCategoryId !== null && !isSelected;

                    return (
                      <circle
                        key={seg.categoryId}
                        cx={center}
                        cy={center}
                        r={radius}
                        fill="none"
                        stroke={seg.color}
                        strokeWidth={isSelected ? 26 : 20}
                        strokeDasharray={seg.strokeDasharray}
                        strokeDashoffset={seg.strokeDashoffset}
                        className={`${styles.donutSegment} ${
                          isDimmed ? styles.segmentDimmed : ""
                        }`}
                        onClick={() => onToggleCategory(seg.categoryId)}
                        role="button"
                        tabIndex={0}
                        aria-label={`${formatCategoryName(seg.categoryName)} ${formatJPY(seg.amount)}`}
                        aria-pressed={isSelected}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            onToggleCategory(seg.categoryId);
                          }
                        }}
                      />
                    );
                  })}
                </g>
              </svg>

              <div className={styles.donutCenter}>
                <span className={styles.centerLabel}>
                  {selectedRow
                    ? formatCategoryName(selectedRow.categoryName)
                    : `${SCOPE_LABELS[scope]} 計`}
                </span>
                <span className={`${styles.centerAmount} tabular`}>
                  {formatJPY(selectedRow ? selectedRow.amount : totalAmount)}
                </span>
                <span className={`${styles.centerShare} tabular`}>
                  {selectedRow
                    ? `${(selectedRow.shareRatio * 100).toFixed(1)}%`
                    : "100.0%"}
                </span>
              </div>
            </div>
          </div>

          <ul className={styles.categoryList}>
            {rows.map((row) => {
              const isSelected = selectedCategoryId === row.categoryId;
              const isDimmed = selectedCategoryId !== null && !isSelected;
              const pct = (row.shareRatio * 100).toFixed(1);

              return (
                <li key={row.categoryId} className={styles.listItem}>
                  <button
                    type="button"
                    className={`${styles.rowBtn} ${
                      isSelected ? styles.rowSelected : ""
                    } ${isDimmed ? styles.rowDimmed : ""}`}
                    onClick={() => onToggleCategory(row.categoryId)}
                    aria-pressed={isSelected}
                  >
                    <div className={styles.rowTop}>
                      <div className={styles.rowLeft}>
                        <span
                          className={styles.colorDot}
                          style={{ backgroundColor: row.color }}
                          aria-hidden="true"
                        />
                        <span className={styles.catName}>
                          {formatCategoryName(row.categoryName)}
                        </span>
                        <span className={`${styles.countBadge} tabular`}>
                          {row.txCount}件
                        </span>
                      </div>

                      <div className={styles.rowRight}>
                        <span className={`${styles.rowAmount} tabular`}>
                          {formatJPY(row.amount)}
                        </span>
                        <span className={`${styles.rowPct} tabular`}>
                          {pct}%
                        </span>
                      </div>
                    </div>

                    <div className={styles.barTrack} aria-hidden="true">
                      <div
                        className={styles.barFill}
                        style={{
                          width: `${Math.max(2, row.shareRatio * 100)}%`,
                          backgroundColor: row.color,
                        }}
                      />
                    </div>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </section>
  );
}

