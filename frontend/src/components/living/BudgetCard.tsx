import { useActionState, useEffect, useState } from "react";
import { Check, RotateCcw } from "lucide-react";
import { api, ApiError } from "../../api/client";
import { formatJPY, type BudgetOverview } from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import styles from "./BudgetCard.module.css";

interface BudgetCardProps {
  year: number;
  month: number;
  budgetOverview: BudgetOverview;
}

interface ActionState {
  error: string | null;
  savedMessage: string | null;
}

export function BudgetCard({ year, month, budgetOverview }: BudgetCardProps) {
  const defaultBudgetVal = budgetOverview.default_budget?.variable_budget ?? null;
  const overrideBudgetVal =
    budgetOverview.monthly_override?.variable_budget ?? null;
  const effectiveBudgetVal = budgetOverview.effective_budget;

  const [defaultInput, setDefaultInput] = useState(
    defaultBudgetVal !== null ? String(defaultBudgetVal) : ""
  );
  const [overrideInput, setOverrideInput] = useState(
    overrideBudgetVal !== null ? String(overrideBudgetVal) : ""
  );

  useEffect(() => {
    setDefaultInput(defaultBudgetVal !== null ? String(defaultBudgetVal) : "");
  }, [defaultBudgetVal]);

  useEffect(() => {
    setOverrideInput(
      overrideBudgetVal !== null ? String(overrideBudgetVal) : ""
    );
  }, [overrideBudgetVal, year, month]);

  const [defaultState, submitDefaultAction, isDefaultPending] = useActionState<
    ActionState,
    FormData
  >(
    async () => {
      const parsed = Number(defaultInput.replace(/,/g, "").trim());
      if (defaultInput.trim() === "" || !Number.isFinite(parsed) || parsed < 0) {
        return {
          error: "デフォルト予算は0円以上の数値で入力してください。",
          savedMessage: null,
        };
      }
      try {
        await api.upsertBudget({
          year: 0,
          month: 0,
          variable_budget: Math.round(parsed),
        });
        invalidateResources("budgets");
        invalidateResources("summary");
        return { error: null, savedMessage: "デフォルト予算を保存しました" };
      } catch (err) {
        return {
          error:
            err instanceof ApiError
              ? err.message
              : "デフォルト予算の保存に失敗しました。",
          savedMessage: null,
        };
      }
    },
    { error: null, savedMessage: null }
  );

  const [overrideState, submitOverrideAction, isOverridePending] =
    useActionState<ActionState, FormData>(
      async () => {
        const parsed = Number(overrideInput.replace(/,/g, "").trim());
        if (
          overrideInput.trim() === "" ||
          !Number.isFinite(parsed) ||
          parsed < 0
        ) {
          return {
            error: "月別予算は0円以上の数値で入力してください。",
            savedMessage: null,
          };
        }
        try {
          await api.upsertBudget({
            year,
            month,
            variable_budget: Math.round(parsed),
          });
          invalidateResources("budgets");
          invalidateResources("summary");
          return {
            error: null,
            savedMessage: `${year}年${month}月の特別予算を保存しました`,
          };
        } catch (err) {
          return {
            error:
              err instanceof ApiError
                ? err.message
                : "月別予算の保存に失敗しました。",
            savedMessage: null,
          };
        }
      },
      { error: null, savedMessage: null }
    );

  async function handleResetMonthlyOverride() {
    try {
      await api.deleteBudget(year, month);
      invalidateResources("budgets");
      invalidateResources("summary");
    } catch {
      // Ignore if already deleted
    }
  }

  return (
    <section className={styles.card} aria-label="月間変動費予算設定">
      <header className={styles.header}>
        <div>
          <span className={styles.eyebrow}>MONTHLY VARIABLE BUDGET</span>
          <h2 className={styles.title}>月間変動費予算</h2>
        </div>

        <div className={styles.effectivePill}>
          <span className={styles.effectiveLabel}>
            {year}年{month}月 適用中
          </span>
          <span className={`${styles.effectiveAmount} tabular`}>
            {effectiveBudgetVal !== null
              ? formatJPY(effectiveBudgetVal)
              : "未設定"}
          </span>
          {overrideBudgetVal !== null && (
            <span className={styles.overrideTag}>月別指定</span>
          )}
        </div>
      </header>

      <div className={styles.grid}>
        {/* Default Budget (0, 0) */}
        <form action={submitDefaultAction} className={styles.panel}>
          <div className={styles.panelHeader}>
            <span className={styles.panelTitle}>毎月デフォルト予算</span>
            <span className={`${styles.currentSub} tabular`}>
              {defaultBudgetVal !== null
                ? `現在 ${formatJPY(defaultBudgetVal)}`
                : "未設定"}
            </span>
          </div>
          <p className={styles.panelDesc}>
            月別の個別設定がないすべての月に自動適用される基準予算です。
          </p>
          <div className={styles.inputRow}>
            <div className={styles.yenInputWrap}>
              <span className={styles.yenPrefix}>¥</span>
              <input
                type="number"
                min={0}
                step={1000}
                placeholder="80000"
                value={defaultInput}
                onChange={(e) => setDefaultInput(e.target.value)}
                className={`${styles.numberInput} tabular`}
              />
            </div>
            <button
              type="submit"
              disabled={isDefaultPending}
              className={styles.saveBtn}
            >
              <Check size={18} strokeWidth={1.75} />
              <span>保存</span>
            </button>
          </div>
          {defaultState.error && (
            <p className={styles.errorText}>{defaultState.error}</p>
          )}
          {defaultState.savedMessage && !defaultState.error && (
            <p className={styles.savedText}>{defaultState.savedMessage}</p>
          )}
        </form>

        {/* Monthly Override (year, month) */}
        <form action={submitOverrideAction} className={styles.panel}>
          <div className={styles.panelHeader}>
            <span className={styles.panelTitle}>
              {year}年{month}月の個別オーバーライド
            </span>
            {overrideBudgetVal !== null ? (
              <button
                type="button"
                onClick={handleResetMonthlyOverride}
                className={styles.resetBtn}
              >
                <RotateCcw size={18} strokeWidth={1.75} />
                <span>デフォルトに戻す</span>
              </button>
            ) : (
              <span className={styles.currentSub}>デフォルト適用中</span>
            )}
          </div>
          <p className={styles.panelDesc}>
            旅行やイベントがある月だけ一時的に変動費予算を変更します。
          </p>
          <div className={styles.inputRow}>
            <div className={styles.yenInputWrap}>
              <span className={styles.yenPrefix}>¥</span>
              <input
                type="number"
                min={0}
                step={1000}
                placeholder={
                  defaultBudgetVal !== null ? String(defaultBudgetVal) : "95000"
                }
                value={overrideInput}
                onChange={(e) => setOverrideInput(e.target.value)}
                className={`${styles.numberInput} tabular`}
              />
            </div>
            <button
              type="submit"
              disabled={isOverridePending}
              className={styles.saveBtn}
            >
              <Check size={18} strokeWidth={1.75} />
              <span>上書き</span>
            </button>
          </div>
          {overrideState.error && (
            <p className={styles.errorText}>{overrideState.error}</p>
          )}
          {overrideState.savedMessage && !overrideState.error && (
            <p className={styles.savedText}>{overrideState.savedMessage}</p>
          )}
        </form>
      </div>
    </section>
  );
}

