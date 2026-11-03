import { useLocation } from "react-router-dom";

// Where shows the current address, so tests can check redirects.
export function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}
