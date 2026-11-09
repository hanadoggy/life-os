import { useActionState, useEffect, useRef, useState } from "react";
import { Check, Trash2, X } from "lucide-react";
import { api, ApiError } from "../../api/client";
import {
  formatCategoryName,
  formatExpenseType,
  formatJPY,
  formatPaymentMethodName,
  type Category,
  type ExpenseType,
  type PaymentMethod,
  type RecurringFrequency,
  type RecurringRule,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import { useSwipeToClose } from "../../hooks/useSwipeToClose";
import styles from "./RecurringRuleDrawer.module.css";

interface RecurringRuleDrawerProps {
  open: boolean;
  editingRule: RecurringRule | null;
  categories: Category[];
  paymentMethods: PaymentMethod[];
  onClose: () => void;
}

interface ActionState {
  error: string | null;
}

export function RecurringRuleDrawer({
  open,
  editingRule,
  categories,
  paymentMethods,
  onClose,
}: RecurringRuleDrawerProps) {
  const dialogRef = useRef<HTMLDialogElement | null>(null);
  const { dragOffsetY, handleProps } = useSwipeToClose(onClose);

  const standardPaymentMethods = paymentMethods.filter(
    (pm) => pm.name !== "ADVANCE"
  );

  const defaultCategoryID =
    categories.find((c) => c.name === "RENT" || c.name === "SUBSCRIPTION")
      ?.id ??
    categories[0]?.id ??
    1;
  const defaultPaymentMethodID = standardPaymentMethods[0]?.id ?? 1;

  const [title, setTitle] = useState("");
  const [expenseType, setExpenseType] = useState<ExpenseType>("FIXED");
  const [categoryID, setCategoryID] = useState<number>(defaultCategoryID);
  const [paymentMethodID, setPaymentMethodID] = useState<number>(
    defaultPaymentMethodID
  );
  const [amount, setAmount] = useState("0");
  const [frequency, setFrequency] = useState<RecurringFrequency>("MONTHLY");
  const [dayOfMonth, setDayOfMonth] = useState<number>(1);
  const [monthOfYear, setMonthOfYear] = useState<number>(1);
  const [autoCreate, setAutoCreate] = useState(true);
  const [isSubscription, setIsSubscription] = useState(false);
  const [isActive, setIsActive] = useState(true);
  const [memo, setMemo] = useState("");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open) {
      if (!dialog.open) {
        dialog.showModal();
      }
      if (editingRule) {
        setTitle(editingRule.title);
        setExpenseType(editingRule.expense_type);
        setCategoryID(editingRule.category_id);
        setPaymentMethodID(editingRule.payment_method_id);
        setAmount(String(editingRule.amount));
        setFrequency(editingRule.frequency);
        setDayOfMonth(editingRule.day_of_month);
        setMonthOfYear(editingRule.month_of_year ?? 1);
        setAutoCreate(editingRule.auto_create);
        setIsSubscription(editingRule.is_subscription);
        setIsActive(editingRule.is_active);
        setMemo(editingRule.memo ?? "");
      } else {
        setTitle("");
        setExpenseType("FIXED");
        setCategoryID(defaultCategoryID);
        setPaymentMethodID(defaultPaymentMethodID);
        setAmount("");
        setFrequency("MONTHLY");
        setDayOfMonth(25);
        setMonthOfYear(1);
        setAutoCreate(true);
        setIsSubscription(false);
        setIsActive(true);
        setMemo("");
      }
    } else if (dialog.open) {
      dialog.close();
    }
  }, [open, editingRule]);

  async function handleDelete() {
    if (!editingRule) return;
    try {
      await api.deleteRecurringRule(editingRule.id);
      invalidateResources("recurring-rules");
      onClose();
    } catch {
      // Ignore
    }
  }

  const [actionState, submitAction, isPending] = useActionState<
    ActionState,
    FormData
  >(
    async () => {
      if (!title.trim()) {
        return { error: "ルール名（例: 家賃、Netflix）を入力してください。" };
      }
      const parsedAmount = Number(amount.replace(/,/g, "").trim());
      if (amount.trim() === "" || !Number.isFinite(parsedAmount) || parsedAmount < 0) {
        return { error: "金額は0円以上の数値で入力してください。" };
      }
      if (dayOfMonth < 1 || dayOfMonth > 31) {
        return { error: "決済日は1〜31の間で指定してください。" };
      }

      try {
        if (editingRule) {
          await api.updateRecurringRule(editingRule.id, {
            title: title.trim(),
            expense_type: expenseType,
            category_id: categoryID,
            amount: Math.round(parsedAmount),
            payment_method_id: paymentMethodID,
            frequency,
            day_of_month: dayOfMonth,
            month_of_year: frequency === "YEARLY" ? monthOfYear : null,
            auto_create: autoCreate,
            is_subscription: isSubscription,
            is_active: isActive,
            memo: memo.trim(),
          });
        } else {
          await api.createRecurringRule({
            title: title.trim(),
            expense_type: expenseType,
            category_id: categoryID,
            amount: Math.round(parsedAmount),
            payment_method_id: paymentMethodID,
            frequency,
            day_of_month: dayOfMonth,
            month_of_year: frequency === "YEARLY" ? monthOfYear : null,
            auto_create: autoCreate,
            is_subscription: isSubscription,
            is_active: isActive,
            memo: memo.trim() || undefined,
          });
        }

        invalidateResources("recurring-rules");
        onClose();
        return { error: null };
      } catch (err) {
        if (err instanceof ApiError) {
          return { error: err.message };
        }
        return { error: "保存中に通信エラーが発生しました。" };
      }
    },
    { error: null }
  );

  const previewAmount = Number(amount.replace(/,/g, "").trim()) || 0;

  return (
    <dialog
      ref={dialogRef}
      className={styles.dialog}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === dialogRef.current) {
          onClose();
        }
      }}
    >
      <div
        className={styles.sheet}
        style={{
          transform: dragOffsetY > 0 ? `translateY(${dragOffsetY}px)` : undefined,
        }}
      >
        <div className={styles.dragArea} {...handleProps}>
          <div className={styles.dragPill} />
          <div className={styles.sheetHeader}>
            <h2 className={styles.sheetTitle}>
              {editingRule ? "定期固定費・サブスクの編集" : "定期ルールの新規追加"}
            </h2>
            <button
              type="button"
              className={styles.closeBtn}
              onClick={onClose}
              aria-label="閉じる"
            >
              <X size={18} strokeWidth={1.75} />
            </button>
          </div>
        </div>

        <form action={submitAction} className={styles.form}>
          {/* Title & Amount */}
          <div className={styles.row2Col}>
            <label className={styles.fieldLabel}>
              <span>ルール名称</span>
              <input
                type="text"
                placeholder="例: 家賃、Netflix、Amazon Prime"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                className={styles.textInput}
                required
              />
            </label>

            <label className={styles.fieldLabel}>
              <span>金額 (円)</span>
              <input
                type="number"
                min={0}
                step={10}
                placeholder="98000"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className={`${styles.textInput} tabular`}
                required
              />
            </label>
          </div>

          {/* Expense Type & Frequency */}
          <div className={styles.row2Col}>
            <div className={styles.fieldLabel}>
              <span>支出区分</span>
              <div className={styles.segmented}>
                {(["FIXED", "VARIABLE"] as ExpenseType[]).map((et) => (
                  <button
                    key={et}
                    type="button"
                    className={`${styles.segBtn} ${
                      expenseType === et ? styles.segActive : ""
                    }`}
                    onClick={() => setExpenseType(et)}
                  >
                    {formatExpenseType(et)}
                  </button>
                ))}
              </div>
            </div>

            <div className={styles.fieldLabel}>
              <span>決済サイクル</span>
              <div className={styles.segmented}>
                <button
                  type="button"
                  className={`${styles.segBtn} ${
                    frequency === "MONTHLY" ? styles.segActive : ""
                  }`}
                  onClick={() => setFrequency("MONTHLY")}
                >
                  毎月 (MONTHLY)
                </button>
                <button
                  type="button"
                  className={`${styles.segBtn} ${
                    frequency === "YEARLY" ? styles.segActive : ""
                  }`}
                  onClick={() => setFrequency("YEARLY")}
                >
                  毎年 (YEARLY)
                </button>
              </div>
            </div>
          </div>

          {/* Billing Day & Optional Month */}
          <div className={styles.row2Col}>
            {frequency === "YEARLY" && (
              <label className={styles.fieldLabel}>
                <span>決済月 (1〜12月)</span>
                <select
                  value={monthOfYear}
                  onChange={(e) => setMonthOfYear(Number(e.target.value))}
                  className={`${styles.selectInput} tabular`}
                >
                  {Array.from({ length: 12 }, (_, idx) => idx + 1).map((m) => (
                    <option key={m} value={m}>
                      {m}月
                    </option>
                  ))}
                </select>
              </label>
            )}

            <label className={styles.fieldLabel}>
              <span>決済日 (31指定で月末自動補正)</span>
              <select
                value={dayOfMonth}
                onChange={(e) => setDayOfMonth(Number(e.target.value))}
                className={`${styles.selectInput} tabular`}
              >
                {Array.from({ length: 31 }, (_, idx) => idx + 1).map((d) => (
                  <option key={d} value={d}>
                    {d === 31 ? "毎月31日 (月末補正)" : `毎月${d}日`}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* Category & Payment Method */}
          <div className={styles.row2Col}>
            <label className={styles.fieldLabel}>
              <span>カテゴリ</span>
              <select
                value={categoryID}
                onChange={(e) => setCategoryID(Number(e.target.value))}
                className={styles.selectInput}
              >
                {categories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {formatCategoryName(c.name)}
                  </option>
                ))}
              </select>
            </label>

            <label className={styles.fieldLabel}>
              <span>支払方法</span>
              <select
                value={paymentMethodID}
                onChange={(e) => setPaymentMethodID(Number(e.target.value))}
                className={styles.selectInput}
              >
                {standardPaymentMethods.map((pm) => (
                  <option key={pm.id} value={pm.id}>
                    {formatPaymentMethodName(pm.name)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* Automation & Timeline Options */}
          <div className={styles.toggleList}>
            <label className={styles.toggleRow}>
              <div>
                <span className={styles.toggleTitle}>
                  決済日に家計簿へ無人自動記帳 (Auto-Create)
                </span>
                <span className={styles.toggleSub}>
                  有効時、指定決済日に家計簿とGoogle Sheetsへ自動で記帳します。
                </span>
              </div>
              <input
                type="checkbox"
                checked={autoCreate}
                onChange={(e) => setAutoCreate(e.target.checked)}
                className={styles.checkbox}
              />
            </label>

            <label className={styles.toggleRow}>
              <div>
                <span className={styles.toggleTitle}>
                  サブスク決済日タイムラインに表示
                </span>
                <span className={styles.toggleSub}>
                  解約忘れ防止のため、次回決済日までの残り日数(D-Day)を表示します。
                </span>
              </div>
              <input
                type="checkbox"
                checked={isSubscription}
                onChange={(e) => setIsSubscription(e.target.checked)}
                className={styles.checkbox}
              />
            </label>

            {editingRule && (
              <label className={styles.toggleRow}>
                <div>
                  <span className={styles.toggleTitle}>ルールを有効化</span>
                  <span className={styles.toggleSub}>
                    一時停止する場合はオフにしてください。
                  </span>
                </div>
                <input
                  type="checkbox"
                  checked={isActive}
                  onChange={(e) => setIsActive(e.target.checked)}
                  className={styles.checkbox}
                />
              </label>
            )}
          </div>

          <label className={styles.fieldLabel}>
            <span>メモ (任意)</span>
            <input
              type="text"
              placeholder="契約プラン・更新条件など"
              value={memo}
              onChange={(e) => setMemo(e.target.value)}
              className={styles.textInput}
            />
          </label>

          {actionState.error && (
            <div className={styles.errorBanner} role="alert">
              {actionState.error}
            </div>
          )}

          <div className={styles.footerActions}>
            {editingRule && (
              <button
                type="button"
                onClick={handleDelete}
                className={styles.deleteBtn}
              >
                <Trash2 size={18} strokeWidth={1.75} />
                <span>削除</span>
              </button>
            )}

            <button
              type="submit"
              disabled={isPending}
              className={styles.submitBtn}
            >
              <Check size={18} strokeWidth={1.75} />
              <span>
                {isPending
                  ? "保存中..."
                  : editingRule
                  ? "変更を保存"
                  : `ルールを登録 (${formatJPY(previewAmount)})`}
              </span>
            </button>
          </div>
        </form>
      </div>
    </dialog>
  );
}

