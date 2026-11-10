import { Suspense, useState } from "react";
import { api } from "../api/client";
import type {
  Category,
  FurusatoDonation,
  PaymentMethod,
  RecurringRule,
} from "../api/types";
import { BudgetCard } from "../components/living/BudgetCard";
import { FurusatoDonationDrawer } from "../components/living/FurusatoDonationDrawer";
import { FurusatoSection } from "../components/living/FurusatoSection";
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

function LivingFurusatoContent({
  year,
  paymentMethods,
}: {
  year: number;
  paymentMethods: PaymentMethod[];
}) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingDonation, setEditingDonation] =
    useState<FurusatoDonation | null>(null);

  const furusatoOverview = useResource(`furusato:${year}`, () =>
    api.getFurusatoOverview(year)
  );

  return (
    <div className={styles.tabContent}>
      <FurusatoSection
        taxYear={year}
        overview={furusatoOverview}
        onAddDonation={() => {
          setEditingDonation(null);
          setDrawerOpen(true);
        }}
        onEditDonation={(donation) => {
          setEditingDonation(donation);
          setDrawerOpen(true);
        }}
      />

      <FurusatoDonationDrawer
        open={drawerOpen}
        taxYear={year}
        editingDonation={editingDonation}
        paymentMethods={paymentMethods}
        onClose={() => {
          setDrawerOpen(false);
          setEditingDonation(null);
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
        <Suspense
          fallback={
            <div className={styles.loadingState}>
              ふるさと納税データを読み込み中...
            </div>
          }
        >
          <LivingFurusatoContent
            year={props.year}
            paymentMethods={props.paymentMethods}
          />
        </Suspense>
      )}
    </div>
  );
}
