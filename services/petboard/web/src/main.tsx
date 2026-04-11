import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import "./index.css";

// One QueryClient for the whole app. Defaults are fine for now: data
// stays fresh for 30 seconds, retried once on failure. SSE-driven cache
// invalidation arrives in task #10 and will replace the timer-based
// refetch entirely.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/petboard">
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
