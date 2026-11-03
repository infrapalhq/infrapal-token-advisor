import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { AppRoutes } from "../App";
import { AgentProvider } from "../ta/agents";

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

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}
