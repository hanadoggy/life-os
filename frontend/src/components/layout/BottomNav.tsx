import { ArrowLeftRight, LayoutGrid, Plus } from "lucide-react";
import { navigate, useRoute } from "../../router/useRoute";
import styles from "./BottomNav.module.css";

interface BottomNavProps {
  onOpenQuickExpense: () => void;
  unsettledCount?: number;
}

export function BottomNav({
  onOpenQuickExpense,
  unsettledCount = 0,
}: BottomNavProps) {
  const currentPath = useRoute();

  return (
    <nav className={styles.nav} aria-label="メインナビゲーション">
      <div className={styles.inner}>
        <button
          type="button"
          className={`${styles.tabBtn} ${
            currentPath === "/" ? styles.active : ""
          }`}
          onClick={() => navigate("/")}
        >
          <LayoutGrid size={18} strokeWidth={1.75} />
          <span className={styles.tabLabel}>ダッシュボード</span>
        </button>

        <button
          type="button"
          className={styles.quickAddBtn}
          onClick={onOpenQuickExpense}
          aria-label="支出を記録"
        >
          <Plus size={18} strokeWidth={1.75} />
          <span>記録</span>
        </button>

        <button
          type="button"
          className={`${styles.tabBtn} ${
            currentPath === "/settlements" ? styles.active : ""
          }`}
          onClick={() => navigate("/settlements")}
        >
          <div className={styles.iconWithBadge}>
            <ArrowLeftRight size={18} strokeWidth={1.75} />
            {unsettledCount > 0 && (
              <span className={`${styles.badgeCount} tabular`}>
                {unsettledCount}
              </span>
            )}
          </div>
          <span className={styles.tabLabel}>精算管理</span>
        </button>
      </div>
    </nav>
  );
}

