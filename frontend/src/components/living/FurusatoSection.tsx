import {
  useActionState,
  useEffect,
  useOptimistic,
  useState,
  useTransition,
} from "react";
import { AlertCircle, Check, Edit3, Plus } from "lucide-react";
import { api, ApiError } from "../../api/client";
import {
  formatFurusatoFamilyStatus,
  formatFurusatoOnestopMethod,
  formatJPY,
  type FurusatoDonation,
  type FurusatoFamilyStatus,
  type FurusatoOverview,
} from "../../api/types";
import { invalidateResources } from "../../hooks/useResource";
import styles from "./FurusatoSection.module.css";

interface FurusatoSectionProps {
  taxYear: number;
  overview: FurusatoOverview;
  onAddDonation: () => void;
  onEditDonation: (donation: FurusatoDonation) => void;
}

interface ProfileActionState {
  error: string | null;
  savedMessage: string | null;
}

const FAMILY_STATUS_OPTIONS: FurusatoFamilyStatus[] = [
  "SINGLE",
  "MARRIED_NO_DEP",
  "MARRIED_DEP_1",
];

export function FurusatoSection({
  taxYear,
  overview,
  onAddDonation,
  onEditDonation,
}: FurusatoSectionProps) {
  const [, startTransition] = useTransition();

  const [grossIncomeInput, setGrossIncomeInput] = useState(
    overview.profile.gross_income > 0
      ? String(overview.profile.gross_income)
      : ""
  );
  const [socialInsuranceInput, setSocialInsuranceInput] = useState(
    overview.profile.social_insurance > 0
      ? String(overview.profile.social_insurance)
      : ""
  );
  const [idecoAnnualInput, setIdecoAnnualInput] = useState(
    overview.profile.ideco_annual > 0
      ? String(overview.profile.ideco_annual)
      : ""
  );
  const [familyStatus, setFamilyStatus] = useState<FurusatoFamilyStatus>(
    overview.profile.family_status
  );

  useEffect(() => {
    setGrossIncomeInput(
      overview.profile.gross_income > 0
        ? String(overview.profile.gross_income)
        : ""
    );
    setSocialInsuranceInput(
      overview.profile.social_insurance > 0
        ? String(overview.profile.social_insurance)
        : ""
    );
    setIdecoAnnualInput(
      overview.profile.ideco_annual > 0
        ? String(overview.profile.ideco_annual)
        : ""
    );
    setFamilyStatus(overview.profile.family_status);
  }, [overview.profile, taxYear]);

  const [profileState, submitProfileAction, isProfilePending] = useActionState<
    ProfileActionState,
    FormData
  >(
    async () => {
      const parsedGross = Number(grossIncomeInput.replace(/,/g, "").trim());
      if (
        grossIncomeInput.trim() === "" ||
        !Number.isFinite(parsedGross) ||
        parsedGross < 0
      ) {
        return {
          error: "想定税込年収は0円以上の数値で入力してください。",
          savedMessage: null,
        };
      }

      const parsedSocial =
        socialInsuranceInput.trim() === ""
          ? 0
          : Number(socialInsuranceInput.replace(/,/g, "").trim());
      if (!Number.isFinite(parsedSocial) || parsedSocial < 0) {
        return {
          error: "社会保険料は0円以上の数値で入力してください。",
          savedMessage: null,
        };
      }

      const parsedIdeco =
        idecoAnnualInput.trim() === ""
          ? 0
          : Number(idecoAnnualInput.replace(/,/g, "").trim());
      if (!Number.isFinite(parsedIdeco) || parsedIdeco < 0) {
        return {
          error: "iDeCo年間掛金は0円以上の数値で入力してください。",
          savedMessage: null,
        };
      }

      try {
        await api.upsertFurusatoProfile(taxYear, {
          gross_income: Math.round(parsedGross),
          social_insurance: Math.round(parsedSocial),
          ideco_annual: Math.round(parsedIdeco),
          family_status: familyStatus,
        });
        invalidateResources("furusato");
        return {
          error: null,
          savedMessage: `${taxYear}年分の控除上限額を更新しました`,
        };
      } catch (err) {
        return {
          error:
            err instanceof ApiError
              ? err.message
              : "プロフィールの保存に失敗しました。",
          savedMessage: null,
        };
      }
    },
    { error: null, savedMessage: null }
  );

  const [optimisticItems, toggleOptimisticItem] = useOptimistic(
    overview.items,
    (
      state: FurusatoDonation[],
      update: { id: string; completed: boolean }
    ): FurusatoDonation[] =>
      state.map((item) =>
        item.id === update.id
          ? {
              ...item,
              onestop_completed: update.completed,
              completed_at: update.completed ? new Date().toISOString() : null,
            }
          : item
      )
  );

  function handleToggleOnestop(donation: FurusatoDonation) {
    const nextCompleted = !donation.onestop_completed;
    startTransition(async () => {
      toggleOptimisticItem({ id: donation.id, completed: nextCompleted });
      try {
        await api.updateFurusatoDonation(donation.id, {
          onestop_completed: nextCompleted,
        });
        invalidateResources("furusato");
      } catch {
        invalidateResources("furusato");
      }
    });
  }

  const estimatedLimit = overview.estimated_limit;
  const donatedTotal = overview.donated_total;
  const remainingLimit = overview.remaining_limit;
  const usageRate =
    estimatedLimit > 0
      ? Math.min(100, Math.round((donatedTotal / estimatedLimit) * 1000) / 10)
      : 0;
  const isOverLimit = estimatedLimit > 0 && remainingLimit < 0;

  const uncompletedCount = optimisticItems.filter(
    (d) => !d.onestop_completed
  ).length;

  return (
    <div className={styles.wrapper}>
      {/* 1. Simplified Limit Estimator & Remaining Gauge Card */}
      <section className={styles.card} aria-label="ふるさと納税 控除上限簡易計算">
        <header className={styles.cardHeader}>
          <div>
            <span className={styles.eyebrow}>
              FURUSATO DEDUCTION LIMIT ESTIMATOR ({taxYear}年分)
            </span>
            <h2 className={styles.title}>控除上限額 簡易シミュレーター</h2>
          </div>
          {overview.calculation_breakdown.social_insurance_estimated &&
            overview.profile.gross_income > 0 && (
              <span className={`${styles.metaBadge} tabular`}>
                社会保険料 15%自動推計 (
                {formatJPY(
                  overview.calculation_breakdown.effective_social_insurance
                )}
                )
              </span>
            )}
        </header>

        {/* Top KPI Metrics & Gauge Bar */}
        <div className={styles.metricsPanel}>
          <div className={styles.kpiGrid}>
            <div className={styles.kpiCell}>
              <span className={styles.kpiLabel}>推計控除上限額 (目安)</span>
              <span className={`${styles.kpiPrimaryValue} tabular`}>
                {formatJPY(estimatedLimit)}
              </span>
            </div>

            <div className={styles.kpiCell}>
              <span className={styles.kpiLabel}>寄付済合計</span>
              <span className={`${styles.kpiValue} tabular`}>
                {formatJPY(donatedTotal)}
              </span>
            </div>

            <div className={styles.kpiCell}>
              <span className={styles.kpiLabel}>残り寄付可能枠</span>
              <span
                className={`${styles.kpiRemainingValue} ${
                  isOverLimit ? styles.overLimitText : ""
                } tabular`}
              >
                {formatJPY(remainingLimit)}
              </span>
            </div>
          </div>

          <div className={styles.gaugeBlock}>
            <div className={styles.gaugeHeader}>
              <span className={styles.gaugeLabel}>寄付枠消化率</span>
              <span className={`${styles.gaugePercent} tabular`}>
                {usageRate.toFixed(1)}%
              </span>
            </div>
            <div className={styles.gaugeTrack}>
              <div
                className={`${styles.gaugeFill} ${
                  isOverLimit ? styles.gaugeFillOver : ""
                }`}
                style={{ width: `${Math.min(100, usageRate)}%` }}
              />
            </div>
          </div>
        </div>

        {/* Income & Deduction Input Form */}
        <form action={submitProfileAction} className={styles.profileForm}>
          <div className={styles.formGrid}>
            <label className={styles.fieldLabel}>
              <span>想定税込年収 (円)</span>
              <div className={styles.yenInputWrap}>
                <span className={styles.yenPrefix}>¥</span>
                <input
                  type="number"
                  min={0}
                  step={100000}
                  placeholder="6000000"
                  value={grossIncomeInput}
                  onChange={(e) => setGrossIncomeInput(e.target.value)}
                  className={`${styles.numberInput} tabular`}
                />
              </div>
            </label>

            <label className={styles.fieldLabel}>
              <span>社会保険料 (空欄で年収の15%自動推計)</span>
              <div className={styles.yenInputWrap}>
                <span className={styles.yenPrefix}>¥</span>
                <input
                  type="number"
                  min={0}
                  step={10000}
                  placeholder="未入力で15%自動計算"
                  value={socialInsuranceInput}
                  onChange={(e) => setSocialInsuranceInput(e.target.value)}
                  className={`${styles.numberInput} tabular`}
                />
              </div>
            </label>

            <label className={styles.fieldLabel}>
              <span>年間 iDeCo 掛金 (円)</span>
              <div className={styles.yenInputWrap}>
                <span className={styles.yenPrefix}>¥</span>
                <input
                  type="number"
                  min={0}
                  step={12000}
                  placeholder="0 (例: 276000)"
                  value={idecoAnnualInput}
                  onChange={(e) => setIdecoAnnualInput(e.target.value)}
                  className={`${styles.numberInput} tabular`}
                />
              </div>
            </label>

            <label className={styles.fieldLabel}>
              <span>扶養・世帯区分</span>
              <select
                value={familyStatus}
                onChange={(e) =>
                  setFamilyStatus(e.target.value as FurusatoFamilyStatus)
                }
                className={styles.selectInput}
              >
                {FAMILY_STATUS_OPTIONS.map((opt) => (
                  <option key={opt} value={opt}>
                    {formatFurusatoFamilyStatus(opt)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className={styles.formFooter}>
            <div className={styles.feedbackArea}>
              {profileState.error && (
                <span className={styles.errorText}>{profileState.error}</span>
              )}
              {profileState.savedMessage && !profileState.error && (
                <span className={styles.savedText}>
                  {profileState.savedMessage}
                </span>
              )}
            </div>

            <button
              type="submit"
              disabled={isProfilePending}
              className={styles.calcBtn}
            >
              <Check size={18} strokeWidth={1.75} />
              <span>
                {isProfilePending ? "計算・保存中..." : "上限額を再計算・保存"}
              </span>
            </button>
          </div>
        </form>

        {/* Formula Version Reference Caption (PHASE2_SPECIFICATION §2.5 & §7.2) */}
        <footer className={styles.formulaFooter}>
          <span className={`${styles.formulaCaption} tabular`}>
            {overview.formula_caption ||
              "※ 2026年(令和8年)度 総務省簡易計算式基準 (2026.04 更新)"}
          </span>
        </footer>
      </section>

      {/* 2. Donations & One-Stop Special Exception Tracker Board */}
      <section
        className={styles.card}
        aria-label="寄付履歴・ワンストップ特例トラッカー"
      >
        <header className={styles.cardHeader}>
          <div>
            <span className={styles.eyebrow}>
              ONE-STOP APPLICATION TRACKER
            </span>
            <div className={styles.titleRow}>
              <h2 className={styles.title}>
                寄付明細・ワンストップ特例トラッカー
              </h2>
              {uncompletedCount > 0 ? (
                <span className={`${styles.deadlineAlertBadge} tabular`}>
                  <AlertCircle size={18} strokeWidth={1.75} />
                  <span>
                    未完了 {uncompletedCount}件 ({taxYear + 1}/1/10 必着)
                  </span>
                </span>
              ) : (
                optimisticItems.length > 0 && (
                  <span className={styles.allCompletedBadge}>
                    全件申請完了済
                  </span>
                )
              )}
            </div>
          </div>

          <button
            type="button"
            onClick={onAddDonation}
            className={styles.addBtn}
          >
            <Plus size={18} strokeWidth={1.75} />
            <span>寄付を追加</span>
          </button>
        </header>

        {optimisticItems.length === 0 ? (
          <div className={styles.emptyState}>
            <p className={styles.emptyTitle}>
              {taxYear}年分のふるさと納税記録はまだありません
            </p>
            <p className={styles.emptySub}>
              寄付先自治体と返礼品を登録すると、オンライン(IAM)・郵送のワンストップ特例申請ステータスを一元管理できます。
            </p>
          </div>
        ) : (
          <ul className={styles.donationList}>
            {optimisticItems.map((item) => {
              const isCompleted = item.onestop_completed;
              return (
                <li
                  key={item.id}
                  className={`${styles.donationRow} ${
                    isCompleted ? styles.rowCompleted : ""
                  }`}
                >
                  <button
                    type="button"
                    className={styles.checkToggleBtn}
                    onClick={() => handleToggleOnestop(item)}
                    aria-pressed={isCompleted}
                    aria-label={
                      isCompleted
                        ? `${item.municipality}の申請を未完了に戻す`
                        : `${item.municipality}の申請を完了にする`
                    }
                  >
                    <span
                      className={`${styles.checkCircle} ${
                        isCompleted ? styles.checkCircleActive : ""
                      }`}
                    >
                      <Check size={18} strokeWidth={1.75} />
                    </span>
                  </button>

                  <div
                    className={styles.donationMain}
                    onClick={() => onEditDonation(item)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onEditDonation(item);
                      }
                    }}
                  >
                    <div className={styles.donationTopLine}>
                      <span className={styles.strikeableMunicipality}>
                        {item.municipality}
                      </span>
                      <span
                        className={`${styles.methodBadge} ${
                          item.onestop_method === "ONLINE_IAM"
                            ? styles.methodOnline
                            : item.onestop_method === "POSTAL"
                            ? styles.methodPostal
                            : styles.methodTaxReturn
                        }`}
                      >
                        {formatFurusatoOnestopMethod(item.onestop_method)}
                      </span>
                      {item.transaction_id && (
                        <span className={styles.ledgerLinkedBadge}>
                          家計簿連動済
                        </span>
                      )}
                    </div>

                    <div className={styles.donationSubLine}>
                      {item.return_gift && (
                        <span className={styles.giftName}>
                          {item.return_gift}
                        </span>
                      )}
                      <span className={`${styles.dateMeta} tabular`}>
                        寄付日 {item.donated_at.replace(/-/g, ".")} · 期限{" "}
                        {item.application_deadline.replace(/-/g, ".")}
                      </span>
                    </div>
                  </div>

                  <div
                    className={styles.donationRight}
                    onClick={() => onEditDonation(item)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onEditDonation(item);
                      }
                    }}
                  >
                    <div className={styles.amountCol}>
                      <span
                        className={`${styles.donationAmount} ${
                          isCompleted ? styles.amountCompleted : ""
                        } tabular`}
                      >
                        {formatJPY(item.amount)}
                      </span>
                      <span className={styles.statusCaption}>
                        {isCompleted ? "申請完了" : "申請未完了"}
                      </span>
                    </div>
                    <Edit3
                      size={18}
                      strokeWidth={1.75}
                      className={styles.editIcon}
                    />
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

