import { Suspense, useEffect, useState } from "react";
import { api } from "../api/client";
import type { Category, PaymentMethod, Transaction } from "../api/types";
import { CategoryBreakdownCard } from "../components/ledger/CategoryBreakdownCard";
import { CumulativeChart } from "../components/ledger/CumulativeChart";
import { MonthlySummaryCard } from "../components/ledger/MonthlySummaryCard";
import { TransactionList } from "../components/ledger/TransactionList";
import { useResource } from "../hooks/useResource";
import { navigate } from "../router/useRoute";

interface DashboardPageProps {
  year: number;
  month: number;
  categories: Category[];
  paymentMethods: PaymentMethod[];
  onEditTransaction: (tx: Transaction) => void;
  onOpenNewExpense: () => void;
}

function DashboardContent({
  year,
  month,
  categories,
  paymentMethods,
  onEditTransaction,
  onOpenNewExpense,
}: DashboardPageProps) {
  const [selectedCategoryId, setSelectedCategoryId] = useState<number | null>(
    null
  );

  // Reset category filter when switching months
  useEffect(() => {
    setSelectedCategoryId(null);
  }, [year, month]);

  const summary = useResource(`summary:${year}-${month}`, () =>
    api.getMonthlySummary(year, month)
  );
  const transactions = useResource(`transactions:${year}-${month}`, () =>
    api.listTransactions(year, month)
  );

  function handleToggleCategory(categoryId: number) {
    setSelectedCategoryId((prev) => (prev === categoryId ? null : categoryId));
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
      <MonthlySummaryCard
        summary={summary}
        onNavigateSettlements={() => navigate("/settlements")}
      />
      <CategoryBreakdownCard
        breakdown={summary.category_breakdown ?? []}
        selectedCategoryId={selectedCategoryId}
        onToggleCategory={handleToggleCategory}
      />
      <CumulativeChart points={summary.daily_variable_cumulative} />
      <TransactionList
        transactions={transactions}
        categories={categories}
        paymentMethods={paymentMethods}
        selectedCategoryId={selectedCategoryId}
        onSelectCategory={setSelectedCategoryId}
        onEditTransaction={onEditTransaction}
        onOpenNewExpense={onOpenNewExpense}
      />
    </div>
  );
}

export function DashboardPage(props: DashboardPageProps) {
  return (
    <Suspense
      fallback={
        <div
          style={{
            padding: "48px 16px",
            textAlign: "center",
            color: "var(--text-muted)",
            fontSize: "0.85rem",
          }}
        >
          月次データを読み込み中...
        </div>
      }
    >
      <DashboardContent {...props} />
    </Suspense>
  );
}

