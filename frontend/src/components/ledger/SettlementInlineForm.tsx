import { Plus, Trash2, Users } from "lucide-react";
import { formatJPY, type SettlementInput } from "../../api/types";
import styles from "./SettlementInlineForm.module.css";

export type SettlementMode = "NONE" | "RECEIVABLE" | "PAYABLE";

interface SettlementInlineFormProps {
  mode: SettlementMode;
  onChangeMode: (mode: SettlementMode) => void;
  netAmount: number;
  settlements: SettlementInput[];
  onChangeSettlements: (next: SettlementInput[]) => void;
  disabled?: boolean;
}

export function SettlementInlineForm({
  mode,
  onChangeMode,
  netAmount,
  settlements,
  onChangeSettlements,
  disabled = false,
}: SettlementInlineFormProps) {
  function handleModeSelect(nextMode: SettlementMode) {
    if (disabled) return;
    onChangeMode(nextMode);
    if (nextMode === "NONE") {
      onChangeSettlements([]);
    } else if (nextMode === "PAYABLE") {
      const existingPeer = settlements[0]?.peer_name || "";
      onChangeSettlements([
        {
          peer_name: existingPeer,
          direction: "PAYABLE",
          amount: Math.max(0, netAmount),
          method: "PayPay",
        },
      ]);
    } else if (nextMode === "RECEIVABLE") {
      if (settlements.length === 0 || settlements[0]?.direction === "PAYABLE") {
        const half = Math.floor(Math.max(0, netAmount) / 2);
        onChangeSettlements([
          {
            peer_name: "",
            direction: "RECEIVABLE",
            amount: half,
            method: "PayPay",
          },
        ]);
      }
    }
  }

  // N-person equal split (myself + (N-1) peers)
  function applyEqualSplit(totalHeadcount: number) {
    if (disabled || totalHeadcount < 2 || netAmount <= 0) return;
    const perPerson = Math.floor(netAmount / totalHeadcount);
    const peerCount = totalHeadcount - 1;
    const next: SettlementInput[] = Array.from({ length: peerCount }, (_, i) => ({
      peer_name: settlements[i]?.peer_name || "",
      direction: "RECEIVABLE",
      amount: perPerson,
      method: "PayPay",
    }));
    onChangeSettlements(next);
  }

  function handleAddPeerRow() {
    if (disabled) return;
    onChangeSettlements([
      ...settlements,
      {
        peer_name: "",
        direction: "RECEIVABLE",
        amount: 0,
        method: "PayPay",
      },
    ]);
  }

  function handleRemoveRow(index: number) {
    if (disabled) return;
    const next = settlements.filter((_, idx) => idx !== index);
    onChangeSettlements(next);
    if (next.length === 0) {
      onChangeMode("NONE");
    }
  }

  function handleUpdateRow(index: number, patch: Partial<SettlementInput>) {
    if (disabled) return;
    const next = settlements.map((row, idx) =>
      idx === index ? { ...row, ...patch } : row
    );
    onChangeSettlements(next);
  }

  return (
    <div className={styles.container}>
      <div className={styles.modeHeader}>
        <div className={styles.modeTitle}>
          <Users size={18} strokeWidth={1.75} />
          <span>PayPay 割り勘・立替精算</span>
        </div>

        <div className={styles.modeTabs}>
          <button
            type="button"
            disabled={disabled}
            className={`${styles.modeBtn} ${mode === "NONE" ? styles.active : ""}`}
            onClick={() => handleModeSelect("NONE")}
          >
            なし
          </button>
          <button
            type="button"
            disabled={disabled}
            className={`${styles.modeBtn} ${
              mode === "RECEIVABLE" ? styles.active : ""
            }`}
            onClick={() => handleModeSelect("RECEIVABLE")}
          >
            自分が支払 (回収)
          </button>
          <button
            type="button"
            disabled={disabled}
            className={`${styles.modeBtn} ${
              mode === "PAYABLE" ? styles.activePayable : ""
            }`}
            onClick={() => handleModeSelect("PAYABLE")}
          >
            同僚が立替 (支払)
          </button>
        </div>
      </div>

      {disabled && (
        <p className={styles.lockedNotice}>
          精算完了済みの項目が含まれているため、金額および精算リストは変更できません。
        </p>
      )}

      {mode === "RECEIVABLE" && (
        <div className={styles.body}>
          <div className={styles.splitToolbar}>
            <span className={styles.splitLabel}>均等割り自動計算:</span>
            <div className={styles.splitBtns}>
              {[2, 3, 4].map((n) => (
                <button
                  key={n}
                  type="button"
                  disabled={disabled || netAmount <= 0}
                  className={`${styles.splitBtn} tabular`}
                  onClick={() => applyEqualSplit(n)}
                >
                  {n}人 ({formatJPY(Math.floor(Math.max(0, netAmount) / n))})
                </button>
              ))}
            </div>
          </div>

          <div className={styles.rows}>
            {settlements.map((st, idx) => (
              <div key={idx} className={styles.row}>
                <input
                  type="text"
                  placeholder="同僚名 (例: エイキ、テツ)"
                  value={st.peer_name}
                  disabled={disabled}
                  onChange={(e) =>
                    handleUpdateRow(idx, { peer_name: e.target.value })
                  }
                  className={styles.peerInput}
                />
                <input
                  type="number"
                  inputMode="numeric"
                  min={1}
                  placeholder="回収額"
                  value={st.amount || ""}
                  disabled={disabled}
                  onChange={(e) =>
                    handleUpdateRow(idx, {
                      amount: Math.max(0, Math.trunc(Number(e.target.value) || 0)),
                    })
                  }
                  className={`${styles.amountInput} tabular`}
                />
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => handleRemoveRow(idx)}
                  className={styles.removeBtn}
                  aria-label="精算行を削除"
                >
                  <Trash2 size={18} strokeWidth={1.75} />
                </button>
              </div>
            ))}
          </div>

          <button
            type="button"
            disabled={disabled}
            className={styles.addRowBtn}
            onClick={handleAddPeerRow}
          >
            <Plus size={18} strokeWidth={1.75} />
            <span>回収メンバーを追加</span>
          </button>
        </div>
      )}

      {mode === "PAYABLE" && (
        <div className={styles.body}>
          <p className={styles.payableHint}>
            支払方法が自動的に「立替 (ADVANCE)」に設定され、自分の負担分（
            <span className="tabular">{formatJPY(Math.max(0, netAmount))}</span>
            ）が未払金として記録されます。
          </p>
          <div className={styles.row}>
            <input
              type="text"
              placeholder="立て替えてくれた同僚名 (例: テツ)"
              value={settlements[0]?.peer_name || ""}
              disabled={disabled}
              onChange={(e) =>
                onChangeSettlements([
                  {
                    peer_name: e.target.value,
                    direction: "PAYABLE",
                    amount: Math.max(0, netAmount),
                    method: "PayPay",
                  },
                ])
              }
              className={styles.peerInput}
            />
            <span className={`${styles.payableAmountFixed} tabular`}>
              {formatJPY(Math.max(0, netAmount))}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

