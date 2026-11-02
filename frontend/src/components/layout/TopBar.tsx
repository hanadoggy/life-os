import { ChevronLeft, ChevronRight, LogOut, Moon, Sun } from "lucide-react";
import type { ThemeMode } from "../../hooks/useTheme";
import styles from "./TopBar.module.css";

interface TopBarProps {
  year: number;
  month: number;
  onPrevMonth: () => void;
  onNextMonth: () => void;
  theme: ThemeMode;
  onToggleTheme: () => void;
  onLogout: () => void;
}

export function TopBar({
  year,
  month,
  onPrevMonth,
  onNextMonth,
  theme,
  onToggleTheme,
  onLogout,
}: TopBarProps) {
  return (
    <header className={styles.header}>
      <div className={styles.brand}>
        <span className={styles.brandMark} aria-hidden="true" />
        <span className={styles.brandTitle}>LIFEOS</span>
      </div>

      <div className={styles.monthSelector}>
        <button
          type="button"
          className={styles.iconBtn}
          onClick={onPrevMonth}
          aria-label="前月"
        >
          <ChevronLeft size={18} strokeWidth={1.75} />
        </button>
        <span className={`${styles.monthLabel} tabular`}>
          {year}.{String(month).padStart(2, "0")}
        </span>
        <button
          type="button"
          className={styles.iconBtn}
          onClick={onNextMonth}
          aria-label="翌月"
        >
          <ChevronRight size={18} strokeWidth={1.75} />
        </button>
      </div>

      <div className={styles.actions}>
        <button
          type="button"
          className={styles.iconBtn}
          onClick={onToggleTheme}
          aria-label={theme === "dark" ? "ライトモードへ切替" : "ダークモードへ切替"}
          title={theme === "dark" ? "ライトモード" : "ダークモード"}
        >
          {theme === "dark" ? (
            <Sun size={18} strokeWidth={1.75} />
          ) : (
            <Moon size={18} strokeWidth={1.75} />
          )}
        </button>
        <button
          type="button"
          className={styles.iconBtn}
          onClick={onLogout}
          aria-label="ログアウト"
          title="ログアウト"
        >
          <LogOut size={18} strokeWidth={1.75} />
        </button>
      </div>
    </header>
  );
}

