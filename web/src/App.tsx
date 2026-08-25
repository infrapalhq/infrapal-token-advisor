import { Outlet } from "react-router-dom";
import { NavRail } from "./components/NavRail";
import { Topbar } from "./components/Topbar";
import { useTheme } from "./lib/theme";

function App() {
  const [theme, toggleTheme] = useTheme();

  return (
    <div className="flex h-full flex-col bg-void text-ink">
      <Topbar theme={theme} onToggleTheme={toggleTheme} />
      <div className="flex min-h-0 flex-1">
        <NavRail theme={theme} onToggleTheme={toggleTheme} />
        <main className="min-w-0 flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export default App;
