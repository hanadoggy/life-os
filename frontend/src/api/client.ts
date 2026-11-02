import type {
  AuthUser,
  Category,
  CreatePresetInput,
  CreateTransactionInput,
  ExpensePreset,
  ExpenseType,
  MonthlySummary,
  PaymentMethod,
  ProblemDetails,
  SettlementItem,
  SettlementsOverview,
  Transaction,
  UpdateTransactionInput,
} from "./types";
import { navigate } from "../router/useRoute";

export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails;

  constructor(problem: ProblemDetails) {
    super(problem.detail || problem.title || `HTTP Error ${problem.status}`);
    this.name = "ApiError";
    this.status = problem.status;
    this.problem = problem;
  }
}

interface RequestOptions {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
  headers?: Record<string, string>;
  skipAuthRedirect?: boolean;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = "GET", body, headers = {}, skipAuthRedirect = false } = options;

  const reqHeaders: Record<string, string> = {
    Accept: "application/json, application/problem+json",
    ...headers,
  };

  if (body !== undefined) {
    reqHeaders["Content-Type"] = "application/json";
  }

  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: reqHeaders,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401 && !skipAuthRedirect) {
    if (window.location.pathname !== "/login") {
      navigate("/login", { replace: true });
    }
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const contentType = res.headers.get("Content-Type") || "";
  if (!res.ok) {
    if (contentType.includes("json")) {
      const prob = (await res.json()) as ProblemDetails;
      throw new ApiError({
        type: prob.type || "about:blank",
        title: prob.title || res.statusText,
        status: prob.status || res.status,
        detail: prob.detail,
        errors: prob.errors,
      });
    }
    throw new ApiError({
      type: "about:blank",
      title: res.statusText || "Request Failed",
      status: res.status,
      detail: await res.text(),
    });
  }

  return (await res.json()) as T;
}

export const api = {
  // Auth
  getMe(): Promise<AuthUser> {
    return request<AuthUser>("/api/v1/auth/me", { skipAuthRedirect: true });
  },
  logout(): Promise<void> {
    return request<void>("/api/v1/auth/logout", { method: "POST" });
  },

  // Reference Data
  async listCategories(): Promise<Category[]> {
    const res = await request<{ items: Category[] }>("/api/v1/categories");
    return res.items;
  },
  async listPaymentMethods(): Promise<PaymentMethod[]> {
    const res = await request<{ items: PaymentMethod[] }>("/api/v1/payment-methods");
    return res.items;
  },

  // Presets
  async listPresets(): Promise<ExpensePreset[]> {
    const res = await request<{ items: ExpensePreset[] }>("/api/v1/presets");
    return res.items;
  },
  createPreset(input: CreatePresetInput): Promise<ExpensePreset> {
    return request<ExpensePreset>("/api/v1/presets", {
      method: "POST",
      body: input,
    });
  },
  deletePreset(id: string): Promise<void> {
    return request<void>(`/api/v1/presets/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // Transactions
  async listTransactions(
    year: number,
    month: number,
    expenseType?: ExpenseType
  ): Promise<Transaction[]> {
    const params = new URLSearchParams({
      year: String(year),
      month: String(month),
    });
    if (expenseType) {
      params.set("expense_type", expenseType);
    }
    const res = await request<{ items: Transaction[] }>(
      `/api/v1/transactions?${params.toString()}`
    );
    return res.items;
  },
  createTransaction(
    idempotencyKey: string,
    input: CreateTransactionInput
  ): Promise<Transaction> {
    return request<Transaction>("/api/v1/transactions", {
      method: "POST",
      headers: {
        "Idempotency-Key": idempotencyKey,
      },
      body: input,
    });
  },
  updateTransaction(
    id: string,
    input: UpdateTransactionInput
  ): Promise<Transaction> {
    return request<Transaction>(`/api/v1/transactions/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: input,
    });
  },
  deleteTransaction(id: string): Promise<void> {
    return request<void>(`/api/v1/transactions/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // Monthly Summary
  getMonthlySummary(year: number, month: number): Promise<MonthlySummary> {
    const params = new URLSearchParams({
      year: String(year),
      month: String(month),
    });
    return request<MonthlySummary>(`/api/v1/ledger/summary?${params.toString()}`);
  },

  // Settlements
  listSettlements(settled?: boolean): Promise<SettlementsOverview> {
    const params = new URLSearchParams();
    if (settled !== undefined) {
      params.set("settled", String(settled));
    }
    const qs = params.toString();
    return request<SettlementsOverview>(
      `/api/v1/settlements${qs ? `?${qs}` : ""}`
    );
  },
  updateSettlementStatus(id: string, settled: boolean): Promise<SettlementItem> {
    return request<SettlementItem>(
      `/api/v1/settlements/${encodeURIComponent(id)}`,
      {
        method: "PATCH",
        body: { settled },
      }
    );
  },
};

