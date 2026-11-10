import { useActionState, useEffect, useRef, useState } from "react";
import { Check, Trash2, X } from "lucide-react";
import { api, ApiError } from "../../api/client";
import {
  formatFurusatoOnestopMethod,
  formatJPY,
  formatPaymentMethodName,
  getTodayJSTDateString,
  type FurusatoDonation,
  type FurusatoOnestopMethod,
  type PaymentMethod,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import { useSwipeToClose } from "../../hooks/useSwipeToClose";
import styles from "./FurusatoDonationDrawer.module.css";

interface FurusatoDonationDrawerProps {
  open: boolean;
  taxYear: number;
  editingDonation: FurusatoDonation | null;
  paymentMethods: PaymentMethod[];
  onClose: () => void;
}

interface ActionState {
  error: string | null;
}

const ONESTOP_METHODS: FurusatoOnestopMethod[] = [
  "ONLINE_IAM",
  "POSTAL",
  "TAX_RETURN",
];

export function FurusatoDonationDrawer({
  open,
  taxYear,
  editingDonation,
  paymentMethods,
  onClose,
}: FurusatoDonationDrawerProps) {
  const dialogRef = useRef<HTMLDialogElement | null>(null);
  const { dragOffsetY, handleProps } = useSwipeToClose(onClose);

  const standardPaymentMethods = paymentMethods.filter(
    (pm) => pm.name !== "ADVANCE"
  );
  const defaultPaymentMethodID =
    standardPaymentMethods.find((pm) => pm.name === "CREDIT_CARD")?.id ??
    standardPaymentMethods[0]?.id ??
    1;

  const [donatedAt, setDonatedAt] = useState(getTodayJSTDateString());
  const [municipality, setMunicipality] = useState("");
  const [returnGift, setReturnGift] = useState("");
  const [amount, setAmount] = useState("");
  const [onestopMethod, setOnestopMethod] =
    useState<FurusatoOnestopMethod>("ONLINE_IAM");
  const [onestopCompleted, setOnestopCompleted] = useState(false);
  const [applicationDeadline, setApplicationDeadline] = useState(
    `${taxYear + 1}-01-10`
  );
  const [createExpense, setCreateExpense] = useState(true);
  const [paymentMethodID, setPaymentMethodID] = useState<number>(
    defaultPaymentMethodID
  );
  const [memo, setMemo] = useState("");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open) {
      if (!dialog.open) {
        dialog.showModal();
      }
      if (editingDonation) {
        setDonatedAt(editingDonation.donated_at);
        setMunicipality(editingDonation.municipality);
        setReturnGift(editingDonation.return_gift);
        setAmount(String(editingDonation.amount));
        setOnestopMethod(editingDonation.onestop_method);
        setOnestopCompleted(editingDonation.onestop_completed);
        setApplicationDeadline(editingDonation.application_deadline);
        setCreateExpense(false);
        setMemo(editingDonation.memo ?? "");
      } else {
        const today = getTodayJSTDateString();
        const defaultDate = today.startsWith(`${taxYear}-`)
          ? today
          : `${taxYear}-11-10`;
        setDonatedAt(defaultDate);
        setMunicipality("");
        setReturnGift("");
        setAmount("");
        setOnestopMethod("ONLINE_IAM");
        setOnestopCompleted(false);
        setApplicationDeadline(`${taxYear + 1}-01-10`);
        setCreateExpense(true);
        setPaymentMethodID(defaultPaymentMethodID);
        setMemo("");
      }
    } else if (dialog.open) {
      dialog.close();
    }
  }, [open, editingDonation, taxYear, defaultPaymentMethodID]);

  async function handleDelete() {
    if (!editingDonation) return;
    try {
      await api.deleteFurusatoDonation(editingDonation.id);
      invalidateResources("furusato");
      if (editingDonation.transaction_id) {
        invalidateResources("transactions");
        invalidateResources("summary");
      }
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
      if (!municipality.trim()) {
        return { error: "寄付先の自治体名（例: 北海道白糠町）を入力してください。" };
      }
      const parsedAmount = Number(amount.replace(/,/g, "").trim());
      if (
        amount.trim() === "" ||
        !Number.isFinite(parsedAmount) ||
        parsedAmount <= 0
      ) {
        return { error: "寄付金額は1円以上の数値で入力してください。" };
      }

      try {
        if (editingDonation) {
          await api.updateFurusatoDonation(editingDonation.id, {
            tax_year: taxYear,
            donated_at: donatedAt,
            municipality: municipality.trim(),
            return_gift: returnGift.trim(),
            amount: Math.round(parsedAmount),
            onestop_method: onestopMethod,
            onestop_completed: onestopCompleted,
            application_deadline: applicationDeadline,
            memo: memo.trim(),
          });
        } else {
          await api.createFurusatoDonation({
            tax_year: taxYear,
            donated_at: donatedAt,
            municipality: municipality.trim(),
            return_gift: returnGift.trim(),
            amount: Math.round(parsedAmount),
            onestop_method: onestopMethod,
            onestop_completed: onestopCompleted,
            application_deadline: applicationDeadline,
            memo: memo.trim() || undefined,
            create_expense: createExpense,
            payment_method_id: createExpense ? paymentMethodID : undefined,
          });
        }

        invalidateResources("furusato");
        if (
          (createExpense && !editingDonation) ||
          Boolean(editingDonation?.transaction_id)
        ) {
          invalidateResources("transactions");
          invalidateResources("summary");
        }
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
          transform:
            dragOffsetY > 0 ? `translateY(${dragOffsetY}px)` : undefined,
        }}
      >
        <div className={styles.dragArea} {...handleProps}>
          <div className={styles.dragPill} />
          <div className={styles.sheetHeader}>
            <h2 className={styles.sheetTitle}>
              {editingDonation
                ? "ふるさと納税 寄付明細の編集"
                : `${taxYear}年 ふるさと納税 寄付登録`}
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
          {/* Municipality & Amount */}
          <div className={styles.row2Col}>
            <label className={styles.fieldLabel}>
              <span>寄付先自治体</span>
              <input
                type="text"
                placeholder="例: 北海道白糠町、宮崎県都城市"
                value={municipality}
                onChange={(e) => setMunicipality(e.target.value)}
                className={styles.textInput}
                required
              />
            </label>

            <label className={styles.fieldLabel}>
              <span>寄付金額 (円)</span>
              <input
                type="number"
                min={1000}
                step={1000}
                placeholder="15000"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className={`${styles.textInput} tabular`}
                required
              />
            </label>
          </div>

          {/* Return Gift */}
          <label className={styles.fieldLabel}>
            <span>返礼品名 (任意)</span>
            <input
              type="text"
              placeholder="例: いくら醤油漬け 500g、さがみのり 10kg"
              value={returnGift}
              onChange={(e) => setReturnGift(e.target.value)}
              className={styles.textInput}
            />
          </label>

          {/* Donated Date & Application Deadline */}
          <div className={styles.row2Col}>
            <label className={styles.fieldLabel}>
              <span>寄付決済日</span>
              <input
                type="date"
                value={donatedAt}
                onChange={(e) => setDonatedAt(e.target.value)}
                className={`${styles.textInput} tabular`}
                required
              />
            </label>

            <label className={styles.fieldLabel}>
              <span>申請期限 (デフォルト 翌年1/10必着)</span>
              <input
                type="date"
                value={applicationDeadline}
                onChange={(e) => setApplicationDeadline(e.target.value)}
                className={`${styles.textInput} tabular`}
                required
              />
            </label>
          </div>

          {/* One-stop Method */}
          <label className={styles.fieldLabel}>
            <span>控除申請方式</span>
            <select
              value={onestopMethod}
              onChange={(e) =>
                setOnestopMethod(e.target.value as FurusatoOnestopMethod)
              }
              className={styles.selectInput}
            >
              {ONESTOP_METHODS.map((m) => (
                <option key={m} value={m}>
                  {formatFurusatoOnestopMethod(m)}
                </option>
              ))}
            </select>
          </label>

          {/* Toggle Options */}
          <div className={styles.toggleList}>
            <label className={styles.toggleRow}>
              <div>
                <span className={styles.toggleTitle}>
                  ワンストップ特例申請（または申告準備）完了済
                </span>
                <span className={styles.toggleSub}>
                  自治体へのオンライン申請または郵送投函が完了したらオンにします。
                </span>
              </div>
              <input
                type="checkbox"
                checked={onestopCompleted}
                onChange={(e) => setOnestopCompleted(e.target.checked)}
                className={styles.checkbox}
              />
            </label>

            {!editingDonation && (
              <label className={styles.toggleRow}>
                <div>
                  <span className={styles.toggleTitle}>
                    家計簿とGoogle Sheetsにも支出として同時記帳
                  </span>
                  <span className={styles.toggleSub}>
                    「公共料金・税金 (PUBLIC_FEES)」カテゴリの変動費として自動登録します。
                  </span>
                </div>
                <input
                  type="checkbox"
                  checked={createExpense}
                  onChange={(e) => setCreateExpense(e.target.checked)}
                  className={styles.checkbox}
                />
              </label>
            )}
          </div>

          {!editingDonation && createExpense && (
            <label className={styles.fieldLabel}>
              <span>家計簿記帳時の支払方法</span>
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
          )}

          <label className={styles.fieldLabel}>
            <span>メモ (ポータルサイト・自治体受付番号など)</span>
            <input
              type="text"
              placeholder="例: 楽天ふるさと納税 / 自治体マイページ対象"
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
            {editingDonation && (
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
                  : editingDonation
                  ? "変更を保存"
                  : `寄付を登録 (${formatJPY(previewAmount)})`}
              </span>
            </button>
          </div>
        </form>
      </div>
    </dialog>
  );
}

