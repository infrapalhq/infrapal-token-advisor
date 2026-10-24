import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Routes, Route, Navigate, useLocation, useParams } from "react-router-dom";
import "./index.css";
import Overview from "./ta/Overview";
import Sessions from "./ta/Sessions";
import SessionPage from "./ta/Session";
import Fixes from "./ta/Fixes";
import Settings from "./ta/Settings";
import { api } from "./ta/api";

// The old Fix, Spend and Trace routes are replaced by the mock's pages. Their
// saved links keep working: /spend and /trace go to Overview and Sessions, and
// /trace?session=<id> opens that session.
function RedirectPreservingQuery({ to }: { to: string }) {
  const location = useLocation();
  return <Navigate to={`${to}${location.search}`} replace />;
}

function TraceRedirect() {
  const location = useLocation();
  const session = new URLSearchParams(location.search).get("session");
  return <Navigate to={session ? `/sessions/${encodeURIComponent(session)}` : "/sessions"} replace />;
}

// VisitRecorder tells the API the Sessions page was opened, so "NEW since your
// last visit" is accurate on the next load.
function VisitRecorder() {
  useEffect(() => {
    api.visit().catch(() => {});
  }, []);
  return null;
}

function SessionRoute() {
  const { id } = useParams();
  return <SessionPage key={id} />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Overview />} />
        <Route path="/sessions" element={<><VisitRecorder /><Sessions /></>} />
        <Route path="/sessions/:id" element={<SessionRoute />} />
        <Route path="/fixes" element={<Fixes />} />
        <Route path="/settings" element={<Settings />} />

        <Route path="/spend" element={<RedirectPreservingQuery to="/" />} />
        <Route path="/trace" element={<TraceRedirect />} />
        <Route path="/waste" element={<RedirectPreservingQuery to="/" />} />
        <Route path="/optimize" element={<RedirectPreservingQuery to="/" />} />
        <Route path="/tools" element={<RedirectPreservingQuery to="/" />} />
        <Route path="/waterfall" element={<TraceRedirect />} />
        <Route path="/search" element={<RedirectPreservingQuery to="/sessions" />} />
        <Route path="/ai-mode" element={<RedirectPreservingQuery to="/settings" />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>,
);
