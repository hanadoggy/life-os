import { Suspense } from "react";
import { api } from "../api/client";
import { SettlementList } from "../components/ledger/SettlementList";
import { useResource } from "../hooks/useResource";

function SettlementsContent() {
  const overview = useResource("settlements:all", () => api.listSettlements());

  return <SettlementList items={overview.items} byPeer={overview.by_peer} />;
}

export function SettlementsPage() {
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
          精算データを読み込み中...
        </div>
      }
    >
      <SettlementsContent />
    </Suspense>
  );
}

