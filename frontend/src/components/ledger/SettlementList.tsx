import { useOptimistic, useState, useTransition } from "react";
import { ArrowDownLeft, ArrowUpRight, Check, Users } from "lucide-react";
import { api } from "../../api/client";
import {
  formatJPY,
  type PeerBalance,
  type SettlementItem,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import styles from "./SettlementList.module.css";

interface SettlementListProps {
  items: SettlementItem[];
  byPeer: PeerBalance[];
}

type StatusFilter = "UNSETTLED" | "ALL";

export function SettlementList({ items, byPeer }: SettlementListProps) {
  const [filter, setFilter] = useState<StatusFilter>("UNSETTLED");
  const [, startTransition] = useTransition();

  const [optimisticItems, toggleOptimisticItem] = useOptimistic(
    items,
    (
      state: SettlementItem[],
      update: { id: string; settled: boolean }
    ): SettlementItem[] =>
      state.map((item) =>
        item.id === update.id
          ? {
              ...item,
              settled_at: update.settled ? new Date().toISOString() : null,
            }
          : item
      )
  );

  function handleToggleStatus(item: SettlementItem) {
    const nextSettled = item.settled_at === null;
    startTransition(async () => {
      toggleOptimisticItem({ id: item.id, settled: nextSettled });
      try {
        await api.updateSettlementStatus(item.id, nextSettled);
        invalidateResources("settlements");
        invalidateResources("summary");
        invalidateResources("transactions");
      } catch {
        invalidateResources("settlements");
      }
    });
  }

  const displayedItems =
    filter === "UNSETTLED"
      ? optimisticItems.filter((i) => i.settled_at === null)
      : optimisticItems;

  return (
    <div className={styles.container}>
      {/* Peer Net Balance Overview */}
      <section className={styles.card} aria-label="相手別の未精算残高">
        <div className={styles.header}>
          <div className={styles.titleGroup}>
            <Users size={18} strokeWidth={1.75} />
            <h2 className={styles.title}>メンバー別 未精算バランス</h2>
          </div>
        </div>

        {byPeer.length === 0 ? (
          <div className={styles.emptyBlock}>
            現在、未精算の残高はありません。
          </div>
        ) : (
          <div className={styles.peerGrid}>
            {byPeer.map((peer) => (
              <div key={peer.peer_name} className={styles.peerCard}>
                <div className={styles.peerTop}>
                  <span className={styles.peerName}>{peer.peer_name}</span>
                  <span
                    className={`${styles.peerNet} ${
                      peer.net >= 0 ? styles.netPositive : styles.netNegative
                    } tabular`}
                  >
                    {peer.net >= 0 ? "+" : ""}
                    {formatJPY(peer.net)}
                  </span>
                </div>

                <div className={styles.peerBreakdown}>
                  <span className={styles.peerSubItem}>
                    <span>受取予定</span>
                    <span className="tabular">{formatJPY(peer.receivable)}</span>
                  </span>
                  <span className={styles.peerSubItem}>
                    <span>支払予定</span>
                    <span className="tabular">{formatJPY(peer.payable)}</span>
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Settlement Items List with One-Tap Toggle */}
      <section className={styles.card} aria-label="精算リスト">
        <div className={styles.header}>
          <h2 className={styles.title}>PayPay / 立替精算明細</h2>

          <div className={styles.filterTabs} role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={filter === "UNSETTLED"}
              className={`${styles.tabBtn} ${
                filter === "UNSETTLED" ? styles.tabActive : ""
              }`}
              onClick={() => setFilter("UNSETTLED")}
            >
              未精算のみ
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={filter === "ALL"}
              className={`${styles.tabBtn} ${
                filter === "ALL" ? styles.tabActive : ""
              }`}
              onClick={() => setFilter("ALL")}
            >
              すべて
            </button>
          </div>
        </div>

        {displayedItems.length === 0 ? (
          <div className={styles.emptyBlock}>
            {filter === "UNSETTLED"
              ? "未精算の項目はすべて完了しています。"
              : "精算履歴がありません。"}
          </div>
        ) : (
          <ul className={styles.list}>
            {displayedItems.map((item) => {
              const isSettled = item.settled_at !== null;
              const isReceivable = item.direction === "RECEIVABLE";

              return (
                <li
                  key={item.id}
                  className={`${styles.row} ${
                    isSettled ? styles.rowSettled : ""
                  }`}
                >
                  <button
                    type="button"
                    className={styles.rowButton}
                    onClick={() => handleToggleStatus(item)}
                    aria-pressed={isSettled}
                  >
                    <span
                      className={`${styles.checkCircle} ${
                        isSettled ? styles.checkCircleActive : ""
                      }`}
                      aria-hidden="true"
                    >
                      <Check size={18} strokeWidth={1.75} />
                    </span>

                    <div className={styles.itemMain}>
                      <div className={styles.itemTopLine}>
                        <span className={styles.peerBadge}>
                          {item.peer_name}
                        </span>
                        <span
                          className={`${styles.directionBadge} ${
                            isReceivable
                              ? styles.dirReceivable
                              : styles.dirPayable
                          }`}
                        >
                          {isReceivable ? (
                            <>
                              <ArrowDownLeft size={18} strokeWidth={1.75} />
                              <span>回収 (受取)</span>
                            </>
                          ) : (
                            <>
                              <ArrowUpRight size={18} strokeWidth={1.75} />
                              <span>支払 (送金)</span>
                            </>
                          )}
                        </span>
                        <span className={`${styles.itemDate} tabular`}>
                          {item.tx_date.replace(/-/g, ".")}
                        </span>
                      </div>

                      <div className={styles.itemBottomLine}>
                        <span className={styles.strikeableTitle}>
                          {item.title}
                        </span>
                        <span className={styles.methodLabel}>{item.method}</span>
                      </div>
                    </div>

                    <div className={styles.amountCol}>
                      <span
                        className={`${styles.itemAmount} ${
                          isSettled ? styles.amountSettled : ""
                        } tabular`}
                      >
                        {formatJPY(item.amount)}
                      </span>
                      <span className={styles.statusText}>
                        {isSettled ? "精算済" : "タップで完了"}
                      </span>
                    </div>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

