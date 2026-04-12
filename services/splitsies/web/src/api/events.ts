import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

export function useLiveUpdates() {
  const qc = useQueryClient();

  useEffect(() => {
    const es = new EventSource("/api/events");
    const invalidate = (...keys: string[]) => {
      keys.forEach((k) => qc.invalidateQueries({ queryKey: [k] }));
    };

    es.addEventListener("expense.created", () =>
      invalidate("expenses", "balances", "timeline", "overview"),
    );
    es.addEventListener("expense.deleted", () =>
      invalidate("expenses", "balances", "timeline", "overview"),
    );
    es.addEventListener("settlement.created", () =>
      invalidate("settlements", "balances", "timeline"),
    );
    es.addEventListener("settlement.deleted", () =>
      invalidate("settlements", "balances", "timeline"),
    );
    es.addEventListener("group.created", () => invalidate("groups"));
    es.addEventListener("group.member_added", () => invalidate("groups"));
    es.addEventListener("user.added", () => invalidate("users"));
    es.addEventListener("user.removed", () => invalidate("users"));

    return () => es.close();
  }, [qc]);
}
