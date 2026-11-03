import { useEffect } from "react";
import { Routes, Route, Navigate, useLocation, useParams } from "react-router-dom";
import Overview from "./ta/Overview";
import Sessions from "./ta/Sessions";
import SessionPage from "./ta/Session";
import Fixes from "./ta/Fixes";
import Settings from "./ta/Settings";
import NotFound from "./ta/NotFound";
import { ErrorBoundary } from "./ErrorBoundary";
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

// ScrollToTop puts every page at its top on arrival, so a page doesn't open
// part-way down because the last one was scrolled.
function ScrollToTop() {
  const { pathname } = useLocation();
  useEffect(() => {
    window.scrollTo(0, 0);
    document.querySelector("main")?.scrollTo?.(0, 0);
  }, [pathname]);
  return null;
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

// AppRoutes is every page, without the router around it, so tests can render
// it at any address.
export function AppRoutes() {
  const { pathname } = useLocation();
  // The boundary is keyed by address, so moving to another page clears a crash
  // on this one.
  return (
    <ErrorBoundary key={pathname}>
      <ScrollToTop />
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
        <Route path="*" element={<NotFound />} />
      </Routes>
    </ErrorBoundary>
  );
}
