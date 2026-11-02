export type TransactionKind = "EXPENSE" | "INCOME";
export type ExpenseType = "FIXED" | "VARIABLE" | "IRREGULAR";
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

export interface MonthlySummary {
  year: number;
  month: number;
  fixed_total: number;
  variable_total: number;
  irregular_total: number;
  income_total: number;
  points_saved: number;
  receivable_outstanding: number;
  payable_outstanding: number;
  daily_variable_cumulative: DailyCumulativePoint[];
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
  IRREGULAR: "非定期",
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

