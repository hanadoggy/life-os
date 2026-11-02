import { useOptimistic, useState, useTransition } from "react";
import { Edit3, Plus, Trash2, Users } from "lucide-react";
import { api } from "../../api/client";
import {
  formatCategoryName,
  formatExpenseType,
  formatJPY,
  formatPaymentMethodName,
  type Category,
  type ExpenseType,
  type PaymentMethod,
  type Transaction,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import styles from "./TransactionList.module.css";

interface TransactionListProps {
  transactions: Transaction[];
  categories: Category[];
  paymentMethods: PaymentMethod[];
  onEditTransaction: (tx: Transaction) => void;
  onOpenNewExpense: () => void;
}

type FilterTab = "ALL" | ExpenseType;

export function TransactionList({
  transactions,
  categories,
  paymentMethods,
  onEditTransaction,
  onOpenNewExpense,
}: TransactionListProps) {
  const [filter, setFilter] = useState<FilterTab>("ALL");
  const [, startTransition] = useTransition();
  const [optimisticTxs, removeOptimisticTx] = useOptimistic(
    transactions,
    (state: Transaction[], deletedId: string) =>
      state.filter((tx) => tx.id !== deletedId)
  );

  const categoryMap = new Map(categories.map((c) => [c.id, c.name]));
  const paymentMethodMap = new Map(paymentMethods.map((p) => [p.id, p.name]));

  const filtered =
    filter === "ALL"
      ? optimisticTxs
      : optimisticTxs.filter((tx) => tx.expense_type === filter);

  function handleDelete(tx: Transaction, e: React.MouseEvent) {
    e.stopPropagation();
    if (!window.confirm(`「${tx.title}」を削除しますか？`)) {
      return;
    }
    startTransition(async () => {
      removeOptimisticTx(tx.id);
      try {
        await api.deleteTransaction(tx.id);
        invalidateResources("transactions");
        invalidateResources("summary");
        invalidateResources("settlements");
      } catch {
        invalidateResources("transactions");
      }
    });
  }

  return (
    <section className={styles.card} aria-label="支出明細一覧">
      <div className={styles.header}>
        <h2 className={styles.title}>支出明細</h2>

        <div className={styles.filterTabs} role="tablist">
          {(
            [
              { key: "ALL", label: "すべて" },
              { key: "VARIABLE", label: "変動費" },
              { key: "FIXED", label: "固定費" },
              { key: "IRREGULAR", label: "非定期" },
            ] as { key: FilterTab; label: string }[]
          ).map((tab) => (
            <button
              key={tab.key}
              type="button"
              role="tab"
              aria-selected={filter === tab.key}
              className={`${styles.tabBtn} ${
                filter === tab.key ? styles.tabActive : ""
              }`}
              onClick={() => setFilter(tab.key)}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className={styles.emptyState}>
          <p className={styles.emptyText}>該当する支出明細がありません。</p>
          <button
            type="button"
            className={styles.emptyAddBtn}
            onClick={onOpenNewExpense}
          >
            <Plus size={18} strokeWidth={1.75} />
            <span>支出を記録する</span>
          </button>
        </div>
      ) : (
        <ul className={styles.list}>
          {filtered.map((tx) => {
            const catCode = categoryMap.get(tx.category_id) ?? "";
            const pmCode = paymentMethodMap.get(tx.payment_method_id) ?? "";

            return (
              <li key={tx.id} className={styles.row}>
                <div
                  className={styles.rowMain}
                  onClick={() => onEditTransaction(tx)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onEditTransaction(tx);
                    }
                  }}
                >
                  <div className={styles.leftCol}>
                    <div className={styles.titleLine}>
                      <span className={`${styles.dateBadge} tabular`}>
                        {tx.tx_date.slice(5).replace("-", "/")}
                      </span>
                      <span className={styles.txTitle}>{tx.title}</span>
                    </div>

                    <div className={styles.metaLine}>
                      <span className={styles.typePill}>
                        {formatExpenseType(tx.expense_type)}
                      </span>
                      <span className={styles.metaItem}>
                        {formatCategoryName(catCode)}
                      </span>
                      <span className={styles.metaSep}>/</span>
                      <span className={styles.metaItem}>
                        {formatPaymentMethodName(pmCode)}
                      </span>

                      {tx.points_used > 0 && (
                        <span className={`${styles.pointTag} tabular`}>
                          P -{formatJPY(tx.points_used)}
                        </span>
                      )}

                      {tx.settlements.length > 0 && (
                        <span className={styles.settlementTag}>
                          <Users size={14} strokeWidth={1.75} />
                          <span>
                            {tx.settlements
                              .map((s) => s.peer_name)
                              .join(", ")}
                          </span>
                        </span>
                      )}
                    </div>
                  </div>

                  <div className={styles.rightCol}>
                    <div className={styles.amountGroup}>
                      <span className={`${styles.myShareAmount} tabular`}>
                        {formatJPY(tx.my_share)}
                      </span>
                      {tx.gross_amount !== tx.my_share && (
                        <span className={`${styles.grossSub} tabular`}>
                          総額 {formatJPY(tx.gross_amount)}
                        </span>
                      )}
                    </div>

                    <div className={styles.rowActions}>
                      <span
                        className={styles.actionIcon}
                        title="編集"
                        aria-hidden="true"
                      >
                        <Edit3 size={16} strokeWidth={1.75} />
                      </span>
                      <button
                        type="button"
                        className={styles.deleteBtn}
                        onClick={(e) => handleDelete(tx, e)}
                        aria-label={`${tx.title}を削除`}
                        title="削除"
                      >
                        <Trash2 size={16} strokeWidth={1.75} />
                      </button>
                    </div>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

