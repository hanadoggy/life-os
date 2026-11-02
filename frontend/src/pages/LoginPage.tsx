import { ArrowRight, ShieldCheck } from "lucide-react";
import styles from "./LoginPage.module.css";

export function LoginPage() {
  return (
    <main className={styles.screen}>
      <div className={styles.card}>
        <div className={styles.header}>
          <div className={styles.brandRow}>
            <span className={styles.brandDot} aria-hidden="true" />
            <span className={styles.brandCode}>LIFEOS // TOKYO</span>
          </div>
          <h1 className={styles.title}>Personal Financial &amp; Life Ledger</h1>
          <p className={styles.subtitle}>
            3秒支出記録・PayPay割り勘精算・ポイント防衛・Google Sheets同期を統合したパーソナルオペレーティングシステム。
          </p>
        </div>

        <div className={styles.specGrid}>
          <div className={styles.specCell}>
            <span className={styles.specLabel}>TIMEZONE</span>
            <span className={`${styles.specValue} tabular`}>Asia/Tokyo (JST)</span>
          </div>
          <div className={styles.specCell}>
            <span className={styles.specLabel}>ACCOUNTING</span>
            <span className={`${styles.specValue} tabular`}>MY SHARE BASIS</span>
          </div>
        </div>

        <div className={styles.actionArea}>
          <a href="/auth/google/login" className={styles.loginBtn}>
            <span>Googleアカウントでログイン</span>
            <ArrowRight size={18} strokeWidth={1.75} />
          </a>

          <div className={styles.securityNote}>
            <ShieldCheck size={18} strokeWidth={1.75} />
            <span>許可されたオーナーアカウントのみアクセス可能です</span>
          </div>
        </div>
      </div>
    </main>
  );
}

