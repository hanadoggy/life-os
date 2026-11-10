export type TransactionKind = "EXPENSE" | "INCOME";
export type ExpenseType = "FIXED" | "VARIABLE";
export type SettlementDirection = "RECEIVABLE" | "PAYABLE";

export interface FieldError {
  field: string;
  message: string;
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  detail?: string;
  errors?: FieldError[];
}

export interface AuthUser {
  email: string;
}

export interface Category {
  id: number;
  name: string;
  sort_order: number;
}

export interface PaymentMethod {
  id: number;
  name: string;
  sort_order: number;
}

export interface ExpensePreset {
  id: string;
  title: string;
  expense_type: ExpenseType;
  category_id: number;
  default_amount: number | null;
  default_payment_method_id: number | null;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface CreatePresetInput {
  title: string;
  expense_type: ExpenseType;
  category_id: number;
  default_amount?: number | null;
  default_payment_method_id?: number | null;
  sort_order?: number;
}

export interface SettlementInput {
  peer_name: string;
  direction: SettlementDirection;
  amount: number;
  method?: string;
  memo?: string;
}

export interface Settlement {
  id: string;
  transaction_id: string;
  peer_name: string;
  direction: SettlementDirection;
  amount: number;
  method: string;
  settled_at: string | null;
  memo?: string;
  created_at: string;
  updated_at: string;
}

export interface Transaction {
  id: string;
  tx_date: string;
  kind: TransactionKind;
  expense_type: ExpenseType | null;
  category_id: number;
  title: string;
  gross_amount: number;
  points_used: number;
  net_amount: number;
  receivable_total: number;
  my_share: number;
  payment_method_id: number;
  memo?: string;
  metadata?: Record<string, unknown>;
  settlements: Settlement[];
  created_at: string;
  updated_at: string;
}

export interface CreateTransactionInput {
  tx_date?: string;
  kind?: TransactionKind;
  expense_type?: ExpenseType | null;
  category_id: number;
  title: string;
  gross_amount: number;
  points_used: number;
  payment_method_id: number;
  memo?: string;
  metadata?: Record<string, unknown>;
  settlements?: SettlementInput[];
}

export interface UpdateTransactionInput {
  tx_date?: string;
  kind?: TransactionKind;
  expense_type?: ExpenseType | null;
  category_id?: number;
  title?: string;
  gross_amount?: number;
  points_used?: number;
  payment_method_id?: number;
  memo?: string;
  metadata?: Record<string, unknown>;
  settlements?: SettlementInput[];
}

export interface DailyCumulativePoint {
  date: string;
  amount: number;
  cumulative: number;
}

export interface CategoryBreakdownItem {
  category_id: number;
  category_name: string;
  fixed_amount: number;
  variable_amount: number;
  total_my_share: number;
  points_saved: number;
  fixed_tx_count: number;
  variable_tx_count: number;
  total_tx_count: number;
}

export interface MonthlySummary {
  year: number;
  month: number;
  fixed_total: number;
  variable_total: number;
  income_total: number;
  points_saved: number;
  receivable_outstanding: number;
  payable_outstanding: number;
  variable_budget: number | null;
  variable_remaining: number | null;
  budget_burn_rate: number | null;
  daily_variable_cumulative: DailyCumulativePoint[];
  category_breakdown: CategoryBreakdownItem[];
}

export interface BudgetItem {
  year: number;
  month: number;
  variable_budget: number;
  memo?: string;
  created_at: string;
  updated_at: string;
}

export interface BudgetOverview {
  year: number;
  month: number;
  default_budget: BudgetItem | null;
  monthly_override: BudgetItem | null;
  effective_budget: number | null;
}

export interface UpsertBudgetInput {
  year: number;
  month: number;
  variable_budget: number;
  memo?: string;
}

export type RecurringFrequency = "MONTHLY" | "YEARLY";

export interface RecurringRule {
  id: string;
  title: string;
  expense_type: ExpenseType;
  category_id: number;
  amount: number;
  payment_method_id: number;
  frequency: RecurringFrequency;
  day_of_month: number;
  month_of_year: number | null;
  auto_create: boolean;
  is_subscription: boolean;
  memo?: string;
  sort_order: number;
  is_active: boolean;
  executed_in_month: boolean;
  transaction_id?: string;
  next_billing_date: string;
  days_until: number;
  created_at: string;
  updated_at: string;
}

export interface RecurringRulesOverview {
  year: number;
  month: number;
  items: RecurringRule[];
}

export interface CreateRecurringRuleInput {
  title: string;
  expense_type?: ExpenseType;
  category_id: number;
  amount: number;
  payment_method_id: number;
  frequency: RecurringFrequency;
  day_of_month: number;
  month_of_year?: number | null;
  auto_create?: boolean;
  is_subscription?: boolean;
  memo?: string;
  sort_order?: number;
  is_active?: boolean;
}

export interface UpdateRecurringRuleInput {
  title?: string;
  expense_type?: ExpenseType;
  category_id?: number;
  amount?: number;
  payment_method_id?: number;
  frequency?: RecurringFrequency;
  day_of_month?: number;
  month_of_year?: number | null;
  auto_create?: boolean;
  is_subscription?: boolean;
  memo?: string;
  sort_order?: number;
  is_active?: boolean;
}

export interface SyncRecurringRulesResult {
  year: number;
  month: number;
  target_period: string;
  created_count: number;
  transaction_ids: string[];
}

export type FurusatoFamilyStatus =
  | "SINGLE"
  | "MARRIED_NO_DEP"
  | "MARRIED_DEP_1";

export type FurusatoOnestopMethod = "ONLINE_IAM" | "POSTAL" | "TAX_RETURN";

export interface FurusatoProfile {
  tax_year: number;
  gross_income: number;
  social_insurance: number;
  ideco_annual: number;
  family_status: FurusatoFamilyStatus;
  updated_at?: string;
}

export interface FurusatoLimitBreakdown {
  gross_income: number;
  effective_social_insurance: number;
  social_insurance_estimated: boolean;
  employment_income_deduction: number;
  taxable_income: number;
  resident_tax_income_portion: number;
  marginal_tax_rate: number;
  estimated_limit: number;
}

export interface FurusatoDonation {
  id: string;
  tax_year: number;
  donated_at: string;
  municipality: string;
  return_gift: string;
  amount: number;
  onestop_method: FurusatoOnestopMethod;
  onestop_completed: boolean;
  completed_at?: string | null;
  application_deadline: string;
  transaction_id?: string | null;
  memo?: string;
  created_at: string;
  updated_at: string;
}

export interface FurusatoOverview {
  tax_year: number;
  formula_label: string;
  formula_updated_at: string;
  formula_caption: string;
  profile: FurusatoProfile;
  estimated_limit: number;
  calculation_breakdown: FurusatoLimitBreakdown;
  donated_total: number;
  remaining_limit: number;
  uncompleted_onestop_count: number;
  items: FurusatoDonation[];
}

export interface UpsertFurusatoProfileInput {
  gross_income: number;
  social_insurance?: number;
  ideco_annual?: number;
  family_status?: FurusatoFamilyStatus;
}

export interface CreateFurusatoDonationInput {
  tax_year?: number;
  donated_at: string;
  municipality: string;
  return_gift?: string;
  amount: number;
  onestop_method: FurusatoOnestopMethod;
  onestop_completed?: boolean;
  application_deadline?: string;
  memo?: string;
  create_expense?: boolean;
  payment_method_id?: number;
}

export interface UpdateFurusatoDonationInput {
  tax_year?: number;
  donated_at?: string;
  municipality?: string;
  return_gift?: string;
  amount?: number;
  onestop_method?: FurusatoOnestopMethod;
  onestop_completed?: boolean;
  application_deadline?: string;
  memo?: string;
}


export interface SettlementItem {
  id: string;
  transaction_id: string;
  tx_date: string;
  title: string;
  peer_name: string;
  direction: SettlementDirection;
  amount: number;
  method: string;
  settled_at: string | null;
  memo?: string;
}

export interface PeerBalance {
  peer_name: string;
  receivable: number;
  payable: number;
  net: number;
}

export interface SettlementsOverview {
  items: SettlementItem[];
  by_peer: PeerBalance[];
}

// ==============================================================================
// Japanese Display Mappers (AGENTS.md §3.3: DB English -> UI Japanese)
// ==============================================================================

const CATEGORY_JA_MAP: Record<string, string> = {
  GROCERIES: "食料品",
  SOCIAL: "交際費",
  HOUSEHOLD: "日用品",
  GAMING: "ゲーム",
  CULTURE: "文化・娯楽",
  RENT: "家賃",
  UTILITIES: "光熱費",
  COMMUNICATION: "通信費",
  TRANSPORT: "交通費",
  SUBSCRIPTION: "サブスク",
  PUBLIC_FEES: "公共料金・税金",
  INVESTMENT: "投資",
  CLOTHING: "衣類",
  FURNITURE_ELECTRONICS: "家具・家電",
  SELF_DEVELOPMENT: "自己啓発",
};

const PAYMENT_METHOD_JA_MAP: Record<string, string> = {
  CREDIT_CARD: "クレジットカード",
  CASH: "現金",
  PAY: "Pay決済",
  POINT: "ポイント",
  ADVANCE: "立替",
};

const EXPENSE_TYPE_JA_MAP: Record<ExpenseType, string> = {
  VARIABLE: "変動費",
  FIXED: "固定費",
};

const FURUSATO_FAMILY_STATUS_JA_MAP: Record<FurusatoFamilyStatus, string> = {
  SINGLE: "独身・共働き（扶養なし）",
  MARRIED_NO_DEP: "夫婦（配偶者控除あり・扶養なし）",
  MARRIED_DEP_1: "夫婦＋扶養1人（16〜18歳等）",
};

const FURUSATO_ONESTOP_METHOD_JA_MAP: Record<FurusatoOnestopMethod, string> = {
  ONLINE_IAM: "オンライン (IAM / マイナポータル)",
  POSTAL: "郵送 (申請書＋本人確認書類)",
  TAX_RETURN: "確定申告",
};

export function formatCategoryName(code: string): string {
  return CATEGORY_JA_MAP[code] ?? code;
}

export function formatPaymentMethodName(code: string): string {
  return PAYMENT_METHOD_JA_MAP[code] ?? code;
}

export function formatExpenseType(type: ExpenseType | null | undefined): string {
  if (!type) return "収入";
  return EXPENSE_TYPE_JA_MAP[type] ?? type;
}

export function formatFurusatoFamilyStatus(status: FurusatoFamilyStatus): string {
  return FURUSATO_FAMILY_STATUS_JA_MAP[status] ?? status;
}

export function formatFurusatoOnestopMethod(method: FurusatoOnestopMethod): string {
  return FURUSATO_ONESTOP_METHOD_JA_MAP[method] ?? method;
}


const jpyFormatter = new Intl.NumberFormat("ja-JP");

export function formatJPY(amount: number): string {
  return `¥${jpyFormatter.format(amount)}`;
}

export function getTodayJSTDateString(): string {
  const parts = new Intl.DateTimeFormat("sv-SE", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
  return parts;
}

export function getCurrentJSTYearMonth(): { year: number; month: number } {
  const today = getTodayJSTDateString();
  const [y, m] = today.split("-").map(Number);
  return { year: y, month: m };
}

