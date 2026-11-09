import { useState } from "react";
import {
  CalendarClock,
  CheckCircle2,
  Edit3,
  Plus,
  RefreshCw,
} from "lucide-react";
import { api, ApiError } from "../../api/client";
import {
  formatCategoryName,
  formatExpenseType,
  formatJPY,
  formatPaymentMethodName,
  type Category,
  type PaymentMethod,
  type RecurringRule,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import styles from "./RecurringRulesSection.module.css";

interface RecurringRulesSectionProps {
  year: number;
  month: number;
  rules: RecurringRule[];
  categories: Category[];
  paymentMethods: PaymentMethod[];
  onAddRule: () => void;
  onEditRule: (rule: RecurringRule) => void;
}

export function RecurringRulesSection({
  year,
  month,
  rules,
  categories,
  paymentMethods,
  onAddRule,
  onEditRule,
}: RecurringRulesSectionProps) {
  const [syncing, setSyncing] = useState(false);
  const [syncFeedback, setSyncFeedback] = useState<string | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);

  const catMap = new Map(categories.map((c) => [c.id, c.name]));
  const pmMap = new Map(paymentMethods.map((p) => [p.id, p.name]));

  // Subscription timeline: active rules with is_subscription === true, sorted by days_until ASC
  const subscriptionTimeline = rules
    .filter((r) => r.is_active && r.is_subscription)
    .slice()
    .sort((a, b) => a.days_until - b.days_until || a.day_of_month - b.day_of_month);

  // Calculate monthly equivalent fixed & subscription totals for active rules
  const monthlyEquivalentTotal = rules
    .filter((r) => r.is_active)
    .reduce(
      (sum, r) =>
        sum + (r.frequency === "YEARLY" ? Math.round(r.amount / 12) : r.amount),
      0
    );

  async function handleSyncCurrentMonth() {
    setSyncing(true);
    setSyncFeedback(null);
    setSyncError(null);
    try {
      const res = await api.syncRecurringRules(year, month);
      invalidateResources("recurring-rules");
      invalidateResources("transactions");
      invalidateResources("summary");
      if (res.created_count > 0) {
        setSyncFeedback(
          `${year}年${month}月分の自動記帳ルール ${res.created_count}件を家計簿とGoogle Sheets同期キューに反映しました。`
        );
      } else {
        setSyncFeedback(
          `${year}年${month}月分の対象ルールはすべて記帳済み、または決済日未到来です。`
        );
      }
    } catch (err) {
      setSyncError(
        err instanceof ApiError
          ? err.message
          : "即時反映中にエラーが発生しました。"
      );
    } finally {
      setSyncing(false);
    }
  }

  return (
    <div className={styles.wrapper}>
      {/* 1. Recurring Rules Management Card */}
      <section className={styles.card} aria-label="定期固定費・サブスク管理">
        <header className={styles.cardHeader}>
          <div>
            <span className={styles.eyebrow}>RECURRING AUTOMATION</span>
            <div className={styles.titleRow}>
              <h2 className={styles.title}>定期固定費・サブスク自動記帳</h2>
              <span className={`${styles.monthlyTotalBadge} tabular`}>
                月換算 {formatJPY(monthlyEquivalentTotal)}
              </span>
            </div>
          </div>

          <div className={styles.actionGroup}>
            <button
              type="button"
              onClick={handleSyncCurrentMonth}
              disabled={syncing}
              className={styles.syncBtn}
            >
              <RefreshCw size={18} strokeWidth={1.75} />
              <span>{syncing ? "反映中..." : "今月分を即時反映"}</span>
            </button>

            <button
              type="button"
              onClick={onAddRule}
              className={styles.addBtn}
            >
              <Plus size={18} strokeWidth={1.75} />
              <span>定期ルール追加</span>
            </button>
          </div>
        </header>

        {syncFeedback && (
          <div className={styles.feedbackBanner}>{syncFeedback}</div>
        )}
        {syncError && (
          <div className={styles.errorBanner} role="alert">
            {syncError}
          </div>
        )}

        {rules.length === 0 ? (
          <div className={styles.emptyState}>
            <p className={styles.emptyTitle}>
              登録されている定期ルールがありません
            </p>
            <p className={styles.emptySub}>
              家賃、通信費、光熱費、サブスクリプションを登録すると毎月・毎年の指定決済日に自動で家計簿とGoogle
              Sheetsへ記帳されます。
            </p>
          </div>
        ) : (
          <div className={styles.ruleList}>
            {rules.map((rule) => {
              const catCode = catMap.get(rule.category_id) ?? "";
              const pmCode = pmMap.get(rule.payment_method_id) ?? "";
              const cycleLabel =
                rule.frequency === "YEARLY"
                  ? `毎年 ${rule.month_of_year ?? 1}月${rule.day_of_month}日`
                  : rule.day_of_month === 31
                  ? "毎月 31日 (月末)"
                  : `毎月 ${rule.day_of_month}日`;

              return (
                <div
                  key={rule.id}
                  className={`${styles.ruleRow} ${
                    !rule.is_active ? styles.ruleInactive : ""
                  }`}
                  onClick={() => onEditRule(rule)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onEditRule(rule);
                    }
                  }}
                >
                  <div className={styles.ruleMain}>
                    <div className={styles.ruleTopLine}>
                      <span className={styles.ruleTitle}>{rule.title}</span>
                      <span className={styles.typeBadge}>
                        {formatExpenseType(rule.expense_type)}
                      </span>
                      {rule.auto_create ? (
                        <span className={styles.autoBadge}>自動記帳</span>
                      ) : (
                        <span className={styles.manualBadge}>手動確認</span>
                      )}
                      {rule.is_subscription && (
                        <span className={styles.subBadge}>サブスク</span>
                      )}
                      {!rule.is_active && (
                        <span className={styles.pausedBadge}>停止中</span>
                      )}
                    </div>

                    <div className={styles.ruleMetaLine}>
                      <span className="tabular">{cycleLabel}</span>
                      <span className={styles.dot}>·</span>
                      <span>{formatCategoryName(catCode)}</span>
                      <span className={styles.dot}>·</span>
                      <span>{formatPaymentMethodName(pmCode)}</span>
                      {rule.memo && (
                        <>
                          <span className={styles.dot}>·</span>
                          <span className={styles.memoText}>{rule.memo}</span>
                        </>
                      )}
                    </div>
                  </div>

                  <div className={styles.ruleRight}>
                    <div className={styles.amountCol}>
                      <span className={`${styles.ruleAmount} tabular`}>
                        {formatJPY(rule.amount)}
                      </span>
                      {rule.executed_in_month ? (
                        <span className={styles.executedStatus}>
                          <CheckCircle2 size={18} strokeWidth={1.75} />
                          <span>
                            {month}月記帳済
                          </span>
                        </span>
                      ) : (
                        <span className={`${styles.nextDateText} tabular`}>
                          次回 {rule.next_billing_date}
                        </span>
                      )}
                    </div>

                    <Edit3
                      size={18}
                      strokeWidth={1.75}
                      className={styles.editIcon}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* 2. Subscription Billing D-Day Timeline */}
      <section
        className={styles.card}
        aria-label="サブスク決済日タイムライン"
      >
        <header className={styles.cardHeader}>
          <div>
            <span className={styles.eyebrow}>SUBSCRIPTION TIMELINE</span>
            <h2 className={styles.title}>サブスク決済日タイムライン</h2>
          </div>
          <span className={`${styles.countCaption} tabular`}>
            稼働中 {subscriptionTimeline.length}件
          </span>
        </header>

        {subscriptionTimeline.length === 0 ? (
          <div className={styles.emptyState}>
            <p className={styles.emptySub}>
              「サブスク決済日タイムラインに表示」をオンにしたルールが次回決済日の近い順(D-Day)でここに並びます。
            </p>
          </div>
        ) : (
          <div className={styles.timelineList}>
            {subscriptionTimeline.map((sub) => {
              const isImminent = sub.days_until <= 3;
              const dLabel =
                sub.days_until === 0 ? "D-0 (本日)" : `D-${sub.days_until}`;

              return (
                <div
                  key={sub.id}
                  className={styles.timelineRow}
                  onClick={() => onEditRule(sub)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onEditRule(sub);
                    }
                  }}
                >
                  <div className={styles.timelineLeft}>
                    <span
                      className={`${styles.dDayBadge} ${
                        isImminent ? styles.dDayImminent : ""
                      } tabular`}
                    >
                      {dLabel}
                    </span>

                    <div className={styles.timelineInfo}>
                      <span className={styles.timelineTitle}>{sub.title}</span>
                      <span className={`${styles.timelineDate} tabular`}>
                        <CalendarClock size={18} strokeWidth={1.75} />
                        <span>
                          次回決済 {sub.next_billing_date} (
                          {sub.frequency === "YEARLY" ? "年額更新" : "月額更新"}
                          )
                        </span>
                      </span>
                    </div>
                  </div>

                  <div className={styles.timelineRight}>
                    <span className={`${styles.timelineAmount} tabular`}>
                      {formatJPY(sub.amount)}
                    </span>
                    {sub.frequency === "YEARLY" && (
                      <span className={`${styles.yearlySubText} tabular`}>
                        月換算 {formatJPY(Math.round(sub.amount / 12))}
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>
    </div>
  );
}

