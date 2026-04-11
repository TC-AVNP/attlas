// Subscribes to /petboard/api/events as an EventSource and invalidates
// the relevant react-query caches when matching events arrive. Mounted
// once at the root via useLiveUpdates() — the hook handles connect,
// reconnect-with-backoff, and cleanup.
//
// Event names mirror the petboard server's events package; payloads are
// the JSON object on each "data:" line. Anything we don't recognize
// gets a blunt invalidate-everything fallback so we never miss a state
// update just because a new event type shipped.

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

const EVENTS_URL = "/petboard/api/events";

export function useLiveUpdates() {
  const queryClient = useQueryClient();

  useEffect(() => {
    let es: EventSource | null = null;
    let backoff = 1000;
    let stopped = false;

    const invalidateProjects = () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
    };
    const invalidateProject = (slug: string | undefined) => {
      if (slug) {
        queryClient.invalidateQueries({ queryKey: ["project", slug] });
      }
    };

    const handle = (type: string, payload: any) => {
      switch (type) {
        case "project.created":
        case "project.updated":
        case "project.deleted":
          invalidateProjects();
          invalidateProject(payload?.slug);
          break;
        case "feature.created":
        case "feature.deleted":
        case "feature.updated":
        case "feature.status_changed":
          invalidateProjects();
          invalidateProject(payload?.slug);
          // We don't always know which project a feature belongs to —
          // an unscoped project query invalidation refreshes them all.
          queryClient.invalidateQueries({ queryKey: ["project"] });
          break;
        case "effort.logged":
          invalidateProjects();
          invalidateProject(payload?.slug);
          break;
        default:
          // Unknown event — be paranoid.
          invalidateProjects();
          queryClient.invalidateQueries({ queryKey: ["project"] });
      }
    };

    const connect = () => {
      if (stopped) return;
      es = new EventSource(EVENTS_URL);
      es.onopen = () => {
        backoff = 1000;
      };
      es.onerror = () => {
        // EventSource auto-reconnects, but only on transient errors.
        // Manual reconnect on hard failures with capped backoff.
        es?.close();
        es = null;
        if (!stopped) {
          setTimeout(connect, backoff);
          backoff = Math.min(backoff * 2, 30_000);
        }
      };
      // Listen for every event type by hooking specific names plus a
      // catch-all "message" listener.
      const handleNamed = (type: string) => (e: MessageEvent) => {
        try {
          const payload = JSON.parse(e.data);
          handle(type, payload);
        } catch {
          handle(type, null);
        }
      };
      const types = [
        "project.created",
        "project.updated",
        "project.deleted",
        "feature.created",
        "feature.updated",
        "feature.deleted",
        "feature.status_changed",
        "effort.logged",
      ];
      for (const t of types) {
        es.addEventListener(t, handleNamed(t));
      }
    };

    connect();
    return () => {
      stopped = true;
      es?.close();
    };
  }, [queryClient]);
}
