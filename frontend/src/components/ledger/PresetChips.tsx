import { BookmarkPlus, X } from "lucide-react";
import { formatJPY, type ExpensePreset } from "../../api/types";
import styles from "./PresetChips.module.css";

interface PresetChipsProps {
  presets: ExpensePreset[];
  onSelectPreset: (preset: ExpensePreset) => void;
  onSaveCurrentAsPreset: () => void;
  onDeletePreset: (id: string) => void;
  canSaveCurrent: boolean;
}

export function PresetChips({
  presets,
  onSelectPreset,
  onSaveCurrentAsPreset,
  onDeletePreset,
  canSaveCurrent,
}: PresetChipsProps) {
  return (
    <div className={styles.wrapper}>
      <div className={styles.header}>
        <span className={styles.label}>クイックプリセット</span>
        {canSaveCurrent && (
          <button
            type="button"
            className={styles.savePresetBtn}
            onClick={onSaveCurrentAsPreset}
          >
            <BookmarkPlus size={18} strokeWidth={1.75} />
            <span>現在の入力を登録</span>
          </button>
        )}
      </div>

      {presets.length === 0 ? (
        <p className={styles.emptyHint}>
          よく使う支出（スーパー、ラーメン会など）を入力後に「現在の入力を登録」でワンタップ化できます。
        </p>
      ) : (
        <div className={styles.chipRow}>
          {presets.map((preset) => (
            <div key={preset.id} className={styles.chipGroup}>
              <button
                type="button"
                className={styles.chipBtn}
                onClick={() => onSelectPreset(preset)}
              >
                <span className={styles.chipTitle}>{preset.title}</span>
                {preset.default_amount !== null && (
                  <span className={`${styles.chipAmount} tabular`}>
                    {formatJPY(preset.default_amount)}
                  </span>
                )}
              </button>
              <button
                type="button"
                className={styles.deleteChipBtn}
                onClick={() => onDeletePreset(preset.id)}
                aria-label={`${preset.title}プリセットを削除`}
              >
                <X size={18} strokeWidth={1.75} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

