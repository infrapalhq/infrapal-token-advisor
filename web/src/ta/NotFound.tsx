import { Link } from "react-router-dom";
import { Shell } from "./Shell";

// NotFound is any address the app has no page for.
export default function NotFound() {
  return (
    <Shell>
      <div className="top-row">
        <p className="eb">Not found</p>
      </div>
      <div className="empty">
        <strong>There is no page here</strong>
        <span>The address may be mistyped, or from an older version.</span>
        <Link className="btn" to="/">← Back to Overview</Link>
      </div>
    </Shell>
  );
}
