import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import { AppRoutes } from "./App";
import { AgentProvider } from "./ta/agents";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AgentProvider>
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
    </AgentProvider>
  </StrictMode>,
);
