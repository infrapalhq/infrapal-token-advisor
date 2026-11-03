import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppRoutes } from "../App";
import { AgentProvider } from "../ta/agents";
import { Where } from "./Where";

// renderAt renders the whole app at a path, the way main.tsx does, with the
// current address shown in a test id so tests can check redirects.
export function renderAt(path: string) {
  return render(
    <AgentProvider>
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </AgentProvider>,
  );
}
