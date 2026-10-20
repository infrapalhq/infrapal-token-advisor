import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Routes, Route, Navigate, useLocation } from "react-router-dom";
import "./index.css";
import App from "./App.tsx";
import Fix from "./routes/Fix.tsx";
import Spend from "./routes/Spend.tsx";
import Trace from "./routes/Trace.tsx";

// RedirectPreservingQuery sends an old path to `to`, carrying over the
// current query string (e.g. a saved /waterfall?session=..&prompt=.. link
// becomes /trace?session=..&prompt=..).
function RedirectPreservingQuery({ to }: { to: string }) {
  const location = useLocation();
  return <Navigate to={`${to}${location.search}`} replace />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Fix />} />
          <Route path="spend" element={<Spend />} />
          <Route path="trace" element={<Trace />} />

          {/* Redirects from the pre-redesign 7-tab layout, so saved links
              and bookmarks keep working. */}
          <Route path="waste" element={<RedirectPreservingQuery to="/" />} />
          <Route path="optimize" element={<RedirectPreservingQuery to="/spend" />} />
          <Route path="tools" element={<RedirectPreservingQuery to="/spend" />} />
          <Route path="sessions" element={<RedirectPreservingQuery to="/spend" />} />
          <Route path="waterfall" element={<RedirectPreservingQuery to="/trace" />} />
          <Route path="search" element={<RedirectPreservingQuery to="/trace" />} />
          <Route path="ai-mode" element={<RedirectPreservingQuery to="/trace" />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
);
