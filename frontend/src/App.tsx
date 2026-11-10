import { Suspense, useEffect, useState } from "react";
import { api } from "./api/client";
import {
  getCurrentJSTYearMonth,
  type AuthUser,
  type Transaction,
} from "./api/types";
import { BottomNav } from "./components/layout/BottomNav";
import { TopBar } from "./components/layout/TopBar";
import { QuickExpenseDrawer } from "./components/ledger/QuickExpenseDrawer";
import { invalidateResources, useResource } from "./hooks/useResource";
import { useTheme } from "./hooks/useTheme";
import { DashboardPage } from "./pages/DashboardPage";
import { LivingPage } from "./pages/LivingPage";
import { LoginPage } from "./pages/LoginPage";
import { SettlementsPage } from "./pages/SettlementsPage";
import { Router } from "./router/Router";
import { navigate, useRoute } from "./router/useRoute";

interface AuthenticatedWorkspaceProps {
  year: number;
  month: number;
  drawerOpen: boolean;
  editingTx: Transaction | null;
  onOpenNewExpense: () => void;
  onEditTransaction: (tx: Transaction) => void;
  onCloseDrawer: () => void;
}

function AuthenticatedWorkspace({
  year,
  month,
  drawerOpen,
  editingTx,
  onOpenNewExpense,
  onEditTransaction,
  onCloseDrawer,
}: AuthenticatedWorkspaceProps) {
  const categories = useResource("ref:categories", () => api.listCategories());
  const paymentMethods = useResource("ref:paymentMethods", () =>
    api.listPaymentMethods()
  );
  const presets = useResource("presets", () => api.listPresets());
  const settlementsOverview = useResource("settlements:all", () =>
    api.listSettlements()
  );

  const unsettledCount = settlementsOverview.items.filter(
    (s) => s.settled_at === null
  ).length;

  return (
    <>
      <main className="page-container">
        <Router
          routes={[
            {
              path: "/",
              render: () => (
                <DashboardPage
                  year={year}
                  month={month}
                  categories={categories}
                  paymentMethods={paymentMethods}
                  onEditTransaction={onEditTransaction}
                  onOpenNewExpense={onOpenNewExpense}
                />
              ),
            },
            {
              path: "/living",
              render: () => (
                <LivingPage
                  year={year}
                  month={month}
                  categories={categories}
                  paymentMethods={paymentMethods}
                />
              ),
            },
            {
              path: "/settlements",
              render: () => <SettlementsPage />,
            },
          ]}
          fallback={() => (
            <DashboardPage
              year={year}
              month={month}
              categories={categories}
              paymentMethods={paymentMethods}
              onEditTransaction={onEditTransaction}
              onOpenNewExpense={onOpenNewExpense}
            />
          )}
        />
      </main>

      <BottomNav
        onOpenQuickExpense={onOpenNewExpense}
        unsettledCount={unsettledCount}
      />

      <QuickExpenseDrawer
        open={drawerOpen}
        editingTransaction={editingTx}
        categories={categories}
        paymentMethods={paymentMethods}
        presets={presets}
        onClose={onCloseDrawer}
      />
    </>
  );
}

export function App() {
  const currentPath = useRoute();
  const { theme, toggleTheme } = useTheme();
  const initialYM = getCurrentJSTYearMonth();

  const [year, setYear] = useState(initialYM.year);
  const [month, setMonth] = useState(initialYM.month);
  const [user, setUser] = useState<AuthUser | null>(null);
  const [authChecking, setAuthChecking] = useState(true);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingTx, setEditingTx] = useState<Transaction | null>(null);

  useEffect(() => {
    let active = true;
    api
      .getMe()
      .then((u) => {
        if (!active) return;
        setUser(u);
        if (window.location.pathname === "/login") {
          navigate("/", { replace: true });
        }
      })
      .catch(() => {
        if (!active) return;
        setUser(null);
        if (window.location.pathname !== "/login") {
          navigate("/login", { replace: true });
        }
      })
      .finally(() => {
        if (active) {
          setAuthChecking(false);
        }
      });

    return () => {
      active = false;
    };
  }, []);

  function handlePrevMonth() {
    if (month === 1) {
      setYear((y) => y - 1);
      setMonth(12);
    } else {
      setMonth((m) => m - 1);
    }
  }

  function handleNextMonth() {
    if (month === 12) {
      setYear((y) => y + 1);
      setMonth(1);
    } else {
      setMonth((m) => m + 1);
    }
  }

  async function handleLogout() {
    try {
      await api.logout();
    } finally {
      setUser(null);
      invalidateResources();
      navigate("/login", { replace: true });
    }
  }

  if (authChecking) {
    return (
      <div
        style={{
          minHeight: "100dvh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: "var(--text-muted)",
          fontSize: "0.85rem",
        }}
      >
        LIFEOSを起動中...
      </div>
    );
  }

  if (!user || currentPath === "/login") {
    return <LoginPage />;
  }

  return (
    <div className="app-shell">
      <TopBar
        year={year}
        month={month}
        onPrevMonth={handlePrevMonth}
        onNextMonth={handleNextMonth}
        theme={theme}
        onToggleTheme={toggleTheme}
        onLogout={handleLogout}
      />

      <Suspense
        fallback={
          <div
            style={{
              padding: "64px 16px",
              textAlign: "center",
              color: "var(--text-muted)",
              fontSize: "0.85rem",
            }}
          >
            ワークスペースを準備中...
          </div>
        }
      >
        <AuthenticatedWorkspace
          year={year}
          month={month}
          drawerOpen={drawerOpen}
          editingTx={editingTx}
          onOpenNewExpense={() => {
            setEditingTx(null);
            setDrawerOpen(true);
          }}
          onEditTransaction={(tx) => {
            setEditingTx(tx);
            setDrawerOpen(true);
          }}
          onCloseDrawer={() => {
            setDrawerOpen(false);
            setEditingTx(null);
          }}
        />
      </Suspense>
    </div>
  );
}

