import { ArrowDownLeft, ArrowUpRight, ShieldCheck } from "lucide-react";
import { formatJPY, type MonthlySummary } from "../../api/types";
import styles from "./MonthlySummaryCard.module.css";

interface MonthlySummaryCardProps {
  summary: MonthlySummary;
  onNavigateSettlements: () => void;
}

export function MonthlySummaryCard({
  summary,
  onNavigateSettlements,
}: MonthlySummaryCardProps) {
  const totalMyShare =
    summary.variable_total + summary.fixed_total + summary.irregular_total;

  return (
    <section className={styles.card} aria-label="月次収支サマリー">
      <div className={styles.heroRow}>
        <div className={styles.heroPrimary}>
          <span className={styles.sectionEyebrow}>
            月間自己負担総額 (MY SHARE)
          </span>
          <div className={`${styles.heroAmount} tabular`}>
            {formatJPY(totalMyShare)}
          </div>
        </div>

        <div className={styles.pointsBadge}>
          <div className={styles.pointsHeader}>
            <ShieldCheck size={18} strokeWidth={1.75} />
            <span>ポイント防衛額</span>
          </div>
          <div className={`${styles.pointsValue} tabular`}>
            {formatJPY(summary.points_saved)}
          </div>
        </div>
      </div>

      <div className={styles.breakdownGrid}>
        <div className={styles.breakdownCell}>
          <span className={styles.cellLabel}>変動費</span>
          <span className={`${styles.cellValue} tabular`}>
            {formatJPY(summary.variable_total)}
          </span>
        </div>

        <div className={styles.breakdownCell}>
          <span className={styles.cellLabel}>固定費</span>
          <span className={`${styles.cellValue} tabular`}>
            {formatJPY(summary.fixed_total)}
          </span>
        </div>

        <div className={styles.breakdownCell}>
          <span className={styles.cellLabel}>非定期</span>
          <span className={`${styles.cellValue} tabular`}>
            {formatJPY(summary.irregular_total)}
          </span>
        </div>
      </div>

      {(summary.receivable_outstanding > 0 ||
        summary.payable_outstanding > 0) && (
        <button
          type="button"
          className={styles.settlementBanner}
          onClick={onNavigateSettlements}
        >
          <div className={styles.settlementGroup}>
            <span className={styles.settlementTag}>
              <ArrowDownLeft size={18} strokeWidth={1.75} />
              <span>未回収 (受取予定)</span>
            </span>
            <span className={`${styles.receivableVal} tabular`}>
              {formatJPY(summary.receivable_outstanding)}
            </span>
          </div>

          <div className={styles.bannerDivider} />

          <div className={styles.settlementGroup}>
            <span className={styles.settlementTag}>
              <ArrowUpRight size={18} strokeWidth={1.75} />
              <span>未払 (送金予定)</span>
            </span>
            <span className={`${styles.payableVal} tabular`}>
              {formatJPY(summary.payable_outstanding)}
            </span>
          </div>
        </button>
      )}
    </section>
  );
}

