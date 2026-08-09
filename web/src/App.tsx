import { Outlet } from "react-router-dom";
import { NavRail } from "./components/NavRail";

function App() {
  return (
    <div className="flex h-full bg-void text-ink">
      <NavRail />
      <main className="min-w-0 flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}

export default App;
