import { Suspense, useState } from "react";
import { api } from "../api/client";
import type { Category, PaymentMethod, RecurringRule } from "../api/types";
import { BudgetCard } from "../components/living/BudgetCard";
import { RecurringRuleDrawer } from "../components/living/RecurringRuleDrawer";
import { RecurringRulesSection } from "../components/living/RecurringRulesSection";
import { useResource } from "../hooks/useResource";
import styles from "./LivingPage.module.css";

interface LivingPageProps {
  year: number;
  month: number;
  categories: Category[];
  paymentMethods: PaymentMethod[];
}

type LivingTab = "RECURRING" | "FURUSATO";

function LivingFixedAndBudgetContent({
  year,
  month,
  categories,
  paymentMethods,
}: LivingPageProps) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<RecurringRule | null>(null);

  const budgetOverview = useResource(`budgets:${year}-${month}`, () =>
    api.getBudgetOverview(year, month)
  );
  const recurringOverview = useResource(
    `recurring-rules:${year}-${month}`,
    () => api.listRecurringRules(year, month)
  );

  return (
    <div className={styles.tabContent}>
      <BudgetCard
        year={year}
        month={month}
        budgetOverview={budgetOverview}
      />

      <RecurringRulesSection
        year={year}
        month={month}
        rules={recurringOverview.items}
        categories={categories}
        paymentMethods={paymentMethods}
        onAddRule={() => {
          setEditingRule(null);
          setDrawerOpen(true);
        }}
        onEditRule={(rule) => {
          setEditingRule(rule);
          setDrawerOpen(true);
        }}
      />

      <RecurringRuleDrawer
        open={drawerOpen}
        editingRule={editingRule}
        categories={categories}
        paymentMethods={paymentMethods}
        onClose={() => {
          setDrawerOpen(false);
          setEditingRule(null);
        }}
      />
    </div>
  );
}

export function LivingPage(props: LivingPageProps) {
  const [activeTab, setActiveTab] = useState<LivingTab>("RECURRING");

  return (
    <div className={styles.page}>
      <div className={styles.tabBar} role="tablist" aria-label="生活管理セクション">
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "RECURRING"}
          className={`${styles.tabBtn} ${
            activeTab === "RECURRING" ? styles.tabActive : ""
          }`}
          onClick={() => setActiveTab("RECURRING")}
        >
          固定費・サブスク・予算
        </button>

        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "FURUSATO"}
          className={`${styles.tabBtn} ${
            activeTab === "FURUSATO" ? styles.tabActive : ""
          }`}
          onClick={() => setActiveTab("FURUSATO")}
        >
          ふるさと納税
        </button>
      </div>

      {activeTab === "RECURRING" ? (
        <Suspense
          fallback={
            <div className={styles.loadingState}>
              固定費・予算データを読み込み中...
            </div>
          }
        >
          <LivingFixedAndBudgetContent {...props} />
        </Suspense>
      ) : (
        <section className={styles.placeholderCard}>
          <span className={styles.placeholderEyebrow}>FURUSATO NOZEI</span>
          <h2 className={styles.placeholderTitle}>
            ふるさと納税 控除上限簡易計算・ワンストップ特例トラッカー
          </h2>
          <p className={styles.placeholderText}>
            ※ Milestone 5 にて実装予定です（2026年度 総務省簡易計算式基準）。
          </p>
        </section>
      )}
    </div>
  );
}

