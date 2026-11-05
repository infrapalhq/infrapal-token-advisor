import { EnableMcp } from "./McpSetup";
import { Shell } from "./Shell";
import { useTheme } from "./theme";

// Settings connects Token Advisor to the user's coding agent over MCP.
export default function Settings() {
  const [theme, toggleTheme] = useTheme();

  return (
    <Shell>
      <div className="top-row">
        <p className="eb">Settings</p>
      </div>
      <section className="section">
        <div className="section-head">
          <h2 className="h2">Claude Code</h2>
        </div>
        <EnableMcp />
      </section>
      <button type="button" className="theme-line" onClick={toggleTheme}>
        {theme === "dark" ? "Light Mode" : "Dark Mode"}
      </button>
    </Shell>
  );
}
