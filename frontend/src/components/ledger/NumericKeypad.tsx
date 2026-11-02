import { Delete } from "lucide-react";
import { formatJPY } from "../../api/types";
import styles from "./NumericKeypad.module.css";

export type AmountTarget = "gross" | "points";

interface NumericKeypadProps {
  activeTarget: AmountTarget;
  onChangeTarget: (target: AmountTarget) => void;
  grossAmount: number;
  pointsUsed: number;
  onChangeGross: (next: number) => void;
  onChangePoints: (next: number) => void;
  showPointsToggle?: boolean;
}

const MAX_AMOUNT = 99_999_999;

export function NumericKeypad({
  activeTarget,
  onChangeTarget,
  grossAmount,
  pointsUsed,
  onChangeGross,
  onChangePoints,
  showPointsToggle = true,
}: NumericKeypadProps) {
  const currentValue = activeTarget === "gross" ? grossAmount : pointsUsed;

  function updateValue(next: number) {
    const clamped = Math.max(0, Math.min(MAX_AMOUNT, Math.trunc(next)));
    if (activeTarget === "gross") {
      onChangeGross(clamped);
    } else {
      onChangePoints(clamped);
    }
  }

  function handleDigit(digits: string) {
    const nextStr = currentValue === 0 ? digits : `${currentValue}${digits}`;
    const parsed = Number(nextStr);
    if (!Number.isNaN(parsed)) {
      updateValue(parsed);
    }
  }

  function handleBackspace() {
    updateValue(Math.floor(currentValue / 10));
  }

  function handleClear() {
    updateValue(0);
  }

  function handleAddQuick(delta: number) {
    updateValue(currentValue + delta);
  }

  return (
    <div className={styles.container}>
      <div className={styles.targetTabs}>
        <button
          type="button"
          className={`${styles.targetTab} ${
            activeTarget === "gross" ? styles.activeGross : ""
          }`}
          onClick={() => onChangeTarget("gross")}
        >
          <span className={styles.targetLabel}>支払総額 (GROSS)</span>
          <span className={`${styles.targetValue} tabular`}>
            {formatJPY(grossAmount)}
          </span>
        </button>

        {showPointsToggle && (
          <button
            type="button"
            className={`${styles.targetTab} ${
              activeTarget === "points" ? styles.activePoints : ""
            }`}
            onClick={() => onChangeTarget("points")}
          >
            <span className={styles.targetLabel}>ポイント利用 (POINTS)</span>
            <span className={`${styles.targetValue} ${styles.pointsValue} tabular`}>
              -{formatJPY(pointsUsed)}
            </span>
          </button>
        )}
      </div>

      <div className={styles.quickAddRow}>
        {[100, 500, 1000, 5000].map((inc) => (
          <button
            key={inc}
            type="button"
            className={`${styles.quickChip} tabular`}
            onClick={() => handleAddQuick(inc)}
          >
            +{inc.toLocaleString("ja-JP")}
          </button>
        ))}
        <button
          type="button"
          className={`${styles.quickChip} ${styles.clearChip}`}
          onClick={handleClear}
        >
          CLR
        </button>
      </div>

      <div className={styles.grid}>
        {["7", "8", "9", "4", "5", "6", "1", "2", "3", "00", "0"].map((key) => (
          <button
            key={key}
            type="button"
            className={`${styles.keyBtn} tabular`}
            onClick={() => handleDigit(key)}
          >
            {key}
          </button>
        ))}
        <button
          type="button"
          className={styles.keyBtn}
          onClick={handleBackspace}
          aria-label="1文字削除"
        >
          <Delete size={18} strokeWidth={1.75} />
        </button>
      </div>
    </div>
  );
}

