import { useActionState, useEffect, useRef, useState } from "react";
import { Check, X } from "lucide-react";
import { api, ApiError } from "../../api/client";
import {
  formatCategoryName,
  formatExpenseType,
  formatJPY,
  formatPaymentMethodName,
  getTodayJSTDateString,
  type Category,
  type ExpensePreset,
  type ExpenseType,
  type PaymentMethod,
  type SettlementInput,
  type Transaction,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import { useSwipeToClose } from "../../hooks/useSwipeToClose";
import { NumericKeypad, type AmountTarget } from "./NumericKeypad";
import { PresetChips } from "./PresetChips";
import { SettlementInlineForm, type SettlementMode } from "./SettlementInlineForm";
import styles from "./QuickExpenseDrawer.module.css";

interface QuickExpenseDrawerProps {
  open: boolean;
  editingTransaction: Transaction | null;
  categories: Category[];
  paymentMethods: PaymentMethod[];
  presets: ExpensePreset[];
  onClose: () => void;
}

interface ActionState {
  error: string | null;
}

export function QuickExpenseDrawer({
  open,
  editingTransaction,
  categories,
  paymentMethods,
  presets,
  onClose,
}: QuickExpenseDrawerProps) {
  const dialogRef = useRef<HTMLDialogElement | null>(null);
  const idempotencyKeyRef = useRef<string>("");

  const standardPaymentMethods = paymentMethods.filter(
    (pm) => pm.name !== "ADVANCE"
  );
  const advancePaymentMethod = paymentMethods.find(
    (pm) => pm.name === "ADVANCE"
  );

  const defaultCategoryID = categories[0]?.id ?? 1;
  const defaultPaymentMethodID = standardPaymentMethods[0]?.id ?? 1;

  const [txDate, setTxDate] = useState(getTodayJSTDateString);
  const [expenseType, setExpenseType] = useState<ExpenseType>("VARIABLE");
  const [categoryID, setCategoryID] = useState<number>(defaultCategoryID);
  const [paymentMethodID, setPaymentMethodID] = useState<number>(
    defaultPaymentMethodID
  );
  const [title, setTitle] = useState("");
  const [memo, setMemo] = useState("");
  const [grossAmount, setGrossAmount] = useState(0);
  const [pointsUsed, setPointsUsed] = useState(0);
  const [amountTarget, setAmountTarget] = useState<AmountTarget>("gross");
  const [settlementMode, setSettlementMode] = useState<SettlementMode>("NONE");
  const [settlements, setSettlements] = useState<SettlementInput[]>([]);

  const { dragOffsetY, handleProps } = useSwipeToClose(onClose);

  // Initialize state whenever the drawer opens or editingTransaction changes
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open) {
      if (!dialog.open) {
        dialog.showModal();
      }
      if (editingTransaction) {
        setTxDate(editingTransaction.tx_date);
        setExpenseType(editingTransaction.expense_type ?? "VARIABLE");
        setCategoryID(editingTransaction.category_id);
        setPaymentMethodID(editingTransaction.payment_method_id);
        setTitle(editingTransaction.title);
        setMemo(editingTransaction.memo ?? "");
        setGrossAmount(editingTransaction.gross_amount);
        setPointsUsed(editingTransaction.points_used);
        setAmountTarget("gross");

        if (editingTransaction.settlements.length > 0) {
          const firstDir = editingTransaction.settlements[0].direction;
          setSettlementMode(firstDir);
          setSettlements(
            editingTransaction.settlements.map((s) => ({
              peer_name: s.peer_name,
              direction: s.direction,
              amount: s.amount,
              method: s.method,
              memo: s.memo,
            }))
          );
        } else {
          setSettlementMode("NONE");
          setSettlements([]);
        }
      } else {
        idempotencyKeyRef.current = crypto.randomUUID();
        setTxDate(getTodayJSTDateString());
        setExpenseType("VARIABLE");
        setCategoryID(categories[0]?.id ?? 1);
        setPaymentMethodID(standardPaymentMethods[0]?.id ?? 1);
        setTitle("");
        setMemo("");
        setGrossAmount(0);
        setPointsUsed(0);
        setAmountTarget("gross");
        setSettlementMode("NONE");
        setSettlements([]);
      }
    } else if (dialog.open) {
      dialog.close();
    }
  }, [open, editingTransaction]);

  const netAmount = Math.max(0, grossAmount - pointsUsed);

  // Keep PAYABLE settlement amount synchronized with netAmount
  useEffect(() => {
    if (settlementMode === "PAYABLE") {
      if (advancePaymentMethod && paymentMethodID !== advancePaymentMethod.id) {
        setPaymentMethodID(advancePaymentMethod.id);
      }
      setSettlements((prev) => [
        {
          peer_name: prev[0]?.peer_name || "",
          direction: "PAYABLE",
          amount: netAmount,
          method: "PayPay",
        },
      ]);
    } else if (
      advancePaymentMethod &&
      paymentMethodID === advancePaymentMethod.id &&
      standardPaymentMethods[0]
    ) {
      setPaymentMethodID(standardPaymentMethods[0].id);
    }
  }, [settlementMode, netAmount]);

  const receivableTotal =
    settlementMode === "RECEIVABLE"
      ? settlements.reduce((acc, s) => acc + (s.amount || 0), 0)
      : 0;
  const myShare = Math.max(0, netAmount - receivableTotal);

  const hasSettledItems =
    editingTransaction?.settlements.some((s) => s.settled_at !== null) ?? false;

  function handleSelectPreset(preset: ExpensePreset) {
    setTitle(preset.title);
    setExpenseType(preset.expense_type);
    setCategoryID(preset.category_id);
    if (preset.default_payment_method_id !== null) {
      setPaymentMethodID(preset.default_payment_method_id);
    }
    if (preset.default_amount !== null && !hasSettledItems) {
      setGrossAmount(preset.default_amount);
    }
  }

  async function handleSaveCurrentAsPreset() {
    if (!title.trim()) return;
    try {
      await api.createPreset({
        title: title.trim(),
        expense_type: expenseType,
        category_id: categoryID,
        default_amount: grossAmount > 0 ? grossAmount : null,
        default_payment_method_id: paymentMethodID,
        sort_order: presets.length * 10 + 10,
      });
      invalidateResources("presets");
    } catch {
      // Ignore duplicate or validation error silently in quick chip save
    }
  }

  async function handleDeletePreset(id: string) {
    try {
      await api.deletePreset(id);
      invalidateResources("presets");
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
        return { error: "内容（タイトル）を入力してください。" };
      }
      if (pointsUsed > grossAmount) {
        return { error: "ポイント利用額が支払総額を超えています。" };
      }
      const activeSettlements =
        settlementMode === "NONE" ? [] : settlements;

      for (const st of activeSettlements) {
        if (!st.peer_name.trim()) {
          return { error: "精算メンバーの名前を入力してください。" };
        }
        if (st.amount <= 0) {
          return { error: "精算金額は1円以上で入力してください。" };
        }
      }
      if (settlementMode === "RECEIVABLE" && receivableTotal > netAmount) {
        return {
          error: `回収合計（${formatJPY(receivableTotal)}）が実支出（${formatJPY(
            netAmount
          )}）を超えています。`,
        };
      }

      try {
        if (editingTransaction) {
          await api.updateTransaction(editingTransaction.id, {
            tx_date: txDate,
            kind: "EXPENSE",
            expense_type: expenseType,
            category_id: categoryID,
            title: title.trim(),
            memo: memo.trim() || undefined,
            ...(hasSettledItems
              ? {}
              : {
                  gross_amount: grossAmount,
                  points_used: pointsUsed,
                  payment_method_id: paymentMethodID,
                  settlements: activeSettlements,
                }),
          });
        } else {
          await api.createTransaction(idempotencyKeyRef.current, {
            tx_date: txDate,
            kind: "EXPENSE",
            expense_type: expenseType,
            category_id: categoryID,
            title: title.trim(),
            gross_amount: grossAmount,
            points_used: pointsUsed,
            payment_method_id: paymentMethodID,
            memo: memo.trim() || undefined,
            settlements: activeSettlements,
          });
        }

        invalidateResources("transactions");
        invalidateResources("summary");
        invalidateResources("settlements");
        onClose();
        return { error: null };
      } catch (err) {
        if (err instanceof ApiError) {
          return { error: err.message };
        }
        return { error: "保存中に通信エラーが発生しました。再試行してください。" };
      }
    },
    { error: null }
  );

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
              {editingTransaction ? "支出の編集" : "クイック支出記録"}
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
          {/* Live Accounting Summary Strip */}
          <div className={styles.accountingBar}>
            <div className={styles.metricCol}>
              <span className={styles.metricLabel}>実支出 (NET)</span>
              <span className={`${styles.metricValue} tabular`}>
                {formatJPY(netAmount)}
              </span>
            </div>
            <div className={styles.metricDivider} />
            <div className={styles.metricCol}>
              <span className={styles.metricLabel}>ポイント防衛</span>
              <span className={`${styles.metricValue} ${styles.accentText} tabular`}>
                {formatJPY(pointsUsed)}
              </span>
            </div>
            <div className={styles.metricDivider} />
            <div className={styles.metricCol}>
              <span className={styles.metricLabel}>自己負担 (MY SHARE)</span>
              <span className={`${styles.metricValue} ${styles.primaryText} tabular`}>
                {formatJPY(myShare)}
              </span>
            </div>
          </div>

          {!editingTransaction && (
            <PresetChips
              presets={presets}
              onSelectPreset={handleSelectPreset}
              onSaveCurrentAsPreset={handleSaveCurrentAsPreset}
              onDeletePreset={handleDeletePreset}
              canSaveCurrent={title.trim().length > 0}
            />
          )}

          {/* Expense Type & Date Row */}
          <div className={styles.row2Col}>
            <div className={styles.segmented}>
              {(["VARIABLE", "FIXED", "IRREGULAR"] as ExpenseType[]).map((et) => (
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

            <input
              type="date"
              value={txDate}
              onChange={(e) => setTxDate(e.target.value)}
              className={`${styles.dateInput} tabular`}
              required
            />
          </div>

          {/* Title & Category & Payment Method */}
          <div className={styles.inputGroup}>
            <input
              type="text"
              placeholder="内容 (例: オリンピック、ラーメン会)"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className={styles.textInput}
              required
            />
          </div>

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
                disabled={settlementMode === "PAYABLE" || hasSettledItems}
                onChange={(e) => setPaymentMethodID(Number(e.target.value))}
                className={styles.selectInput}
              >
                {(settlementMode === "PAYABLE"
                  ? paymentMethods
                  : standardPaymentMethods
                ).map((pm) => (
                  <option key={pm.id} value={pm.id}>
                    {formatPaymentMethodName(pm.name)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* Custom Numeric Keypad (disabled if editing already-settled transaction) */}
          {!hasSettledItems && (
            <NumericKeypad
              activeTarget={amountTarget}
              onChangeTarget={setAmountTarget}
              grossAmount={grossAmount}
              pointsUsed={pointsUsed}
              onChangeGross={setGrossAmount}
              onChangePoints={setPointsUsed}
            />
          )}

          {/* PayPay Settlement Inline Section */}
          <SettlementInlineForm
            mode={settlementMode}
            onChangeMode={setSettlementMode}
            netAmount={netAmount}
            settlements={settlements}
            onChangeSettlements={setSettlements}
            disabled={hasSettledItems}
          />

          <input
            type="text"
            placeholder="メモ (任意)"
            value={memo}
            onChange={(e) => setMemo(e.target.value)}
            className={styles.memoInput}
          />

          {actionState.error && (
            <div className={styles.errorBanner} role="alert">
              {actionState.error}
            </div>
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
                : editingTransaction
                ? "変更を保存"
                : `記録する（自己負担 ${formatJPY(myShare)}）`}
            </span>
          </button>
        </form>
      </div>
    </dialog>
  );
}

